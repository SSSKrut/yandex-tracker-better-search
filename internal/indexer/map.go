package indexer

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gonum.org/v1/gonum/mat"
)

type MapOptions struct {
	MaxIssues            int
	MaxFiles             int
	MaxFileNamesPerIssue int
	MaxDocChars          int
	MaxVocab             int
	MaxNeighbors         int
	SimilarityDims       int
	ClusterK             int
}

type MapNeighbor struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

type MapPoint struct {
	ID             string        `json:"id"`
	Kind           string        `json:"kind"`
	Key            string        `json:"key"`
	Title          string        `json:"title"`
	URL            string        `json:"url"`
	ParentIssueURL string        `json:"parent_issue_url,omitempty"`
	X              float64       `json:"x"`
	Y              float64       `json:"y"`
	Cluster        int           `json:"cluster"`
	Neighbors      []MapNeighbor `json:"neighbors"`
}

type MapStats struct {
	Issues   int `json:"issues"`
	Files    int `json:"files"`
	Points   int `json:"points"`
	Vocab    int `json:"vocab"`
	Clusters int `json:"clusters"`
}

// MapCluster - per-cluster summary precomputed in BuildSimilarityMap.
// CentralKeys lists up to 3 doc keys closest to the cluster centroid in 2D
// space, ordered by centrality. TopKeywords are the top tokens by cumulative
// TF-IDF weight inside the cluster.
type MapCluster struct {
	ID          int      `json:"id"`
	Size        int      `json:"size"`
	TopKeywords []string `json:"top_keywords"`
	CentralKeys []string `json:"central_keys"`
}

type MapData struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Points      []MapPoint   `json:"points"`
	Stats       MapStats     `json:"stats"`
	Clusters    []MapCluster `json:"clusters,omitempty"`
}

type mapDocument struct {
	ID             string
	Kind           string
	Key            string
	Title          string
	URL            string
	ParentIssueURL string
	Text           string
}

func DefaultMapOptions() MapOptions {
	return MapOptions{
		MaxIssues:            1000,
		MaxFiles:             1000,
		MaxFileNamesPerIssue: 5,
		MaxDocChars:          4000,
		MaxVocab:             1000,
		MaxNeighbors:         5,
		SimilarityDims:       16,
		ClusterK:             0,
	}
}

func MapOptionsFromEnv() MapOptions {
	opts := DefaultMapOptions()
	opts.MaxIssues = readEnvInt("MAP_MAX_ISSUES", opts.MaxIssues)
	opts.MaxFiles = readEnvInt("MAP_MAX_FILES", opts.MaxFiles)
	opts.MaxFileNamesPerIssue = readEnvInt("MAP_MAX_FILE_NAMES", opts.MaxFileNamesPerIssue)
	opts.MaxDocChars = readEnvInt("MAP_MAX_DOC_CHARS", opts.MaxDocChars)
	opts.MaxVocab = readEnvInt("MAP_MAX_VOCAB", opts.MaxVocab)
	opts.MaxNeighbors = readEnvInt("MAP_MAX_NEIGHBORS", opts.MaxNeighbors)
	opts.SimilarityDims = readEnvInt("MAP_SIM_DIMS", opts.SimilarityDims)
	opts.ClusterK = readEnvInt("MAP_CLUSTER_K", opts.ClusterK)
	return opts
}

func readEnvInt(name string, fallback int) int {
	val := strings.TrimSpace(os.Getenv(name))
	if val == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(val)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func (idx *Indexer) BuildSimilarityMap(ctx context.Context, opts MapOptions) (*MapData, error) {
	opts = sanitizeMapOptions(opts)

	issueFileNames, err := idx.loadIssueFileNames(ctx, opts)
	if err != nil {
		return nil, err
	}

	issues, err := idx.loadIssueDocs(ctx, opts, issueFileNames)
	if err != nil {
		return nil, err
	}

	files, err := idx.loadFileDocs(ctx, opts)
	if err != nil {
		return nil, err
	}

	docs := append(issues, files...)
	if len(docs) == 0 {
		return &MapData{
			GeneratedAt: time.Now(),
			Stats: MapStats{
				Issues: len(issues),
				Files:  len(files),
				Points: 0,
			},
		}, nil
	}

	matrix, vocab, rows, cols, err := buildTFIDFMatrix(docs, opts.MaxVocab)
	if err != nil {
		return nil, err
	}

	coords, reduced, err := reduceWithSVD(matrix, rows, cols, opts.SimilarityDims)
	if err != nil {
		return nil, err
	}

	clusters := cluster2D(coords, opts.ClusterK)
	neighbors := buildNeighbors(reduced, docs, opts.MaxNeighbors)

	points := make([]MapPoint, len(docs))
	for i, doc := range docs {
		points[i] = MapPoint{
			ID:             doc.ID,
			Kind:           doc.Kind,
			Key:            doc.Key,
			Title:          doc.Title,
			URL:            doc.URL,
			ParentIssueURL: doc.ParentIssueURL,
			X:              coords[i][0],
			Y:              coords[i][1],
			Cluster:        clusters[i],
			Neighbors:      neighbors[i],
		}
	}

	clusterSummaries := summarizeClusters(docs, matrix, vocab, rows, cols, clusters, coords)

	return &MapData{
		GeneratedAt: time.Now(),
		Points:      points,
		Stats: MapStats{
			Issues:   len(issues),
			Files:    len(files),
			Points:   len(docs),
			Vocab:    cols,
			Clusters: uniqueClusterCount(clusters),
		},
		Clusters: clusterSummaries,
	}, nil
}

// summarizeClusters builds per-cluster summaries: top keywords by cumulative
// TF-IDF weight and up to 3 keys closest to the 2D centroid.
func summarizeClusters(
	docs []mapDocument,
	tfidf []float64,
	vocab []string,
	rows, cols int,
	assignments []int,
	coords [][2]float64,
) []MapCluster {
	if rows == 0 || cols == 0 || len(vocab) != cols {
		return nil
	}

	const topKeywordsN = 5
	const centralKeysN = 3

	groups := map[int][]int{}
	for i, c := range assignments {
		groups[c] = append(groups[c], i)
	}

	ids := make([]int, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	out := make([]MapCluster, 0, len(ids))
	for _, id := range ids {
		members := groups[id]
		if len(members) == 0 {
			continue
		}

		weights := make([]float64, cols)
		var cx, cy float64
		for _, i := range members {
			row := tfidf[i*cols : (i+1)*cols]
			for j, v := range row {
				weights[j] += v
			}
			cx += coords[i][0]
			cy += coords[i][1]
		}
		cx /= float64(len(members))
		cy /= float64(len(members))

		type tw struct {
			token  string
			weight float64
		}
		ranked := make([]tw, 0, cols)
		for j, w := range weights {
			if w > 0 {
				ranked = append(ranked, tw{token: vocab[j], weight: w})
			}
		}
		sort.Slice(ranked, func(a, b int) bool {
			if ranked[a].weight == ranked[b].weight {
				return ranked[a].token < ranked[b].token
			}
			return ranked[a].weight > ranked[b].weight
		})
		topN := topKeywordsN
		if len(ranked) < topN {
			topN = len(ranked)
		}
		keywords := make([]string, topN)
		for i := 0; i < topN; i++ {
			keywords[i] = ranked[i].token
		}

		type dk struct {
			idx  int
			dist float64
		}
		dist := make([]dk, len(members))
		for i, m := range members {
			dx := coords[m][0] - cx
			dy := coords[m][1] - cy
			dist[i] = dk{idx: m, dist: dx*dx + dy*dy}
		}
		sort.Slice(dist, func(a, b int) bool { return dist[a].dist < dist[b].dist })
		centralN := centralKeysN
		if len(dist) < centralN {
			centralN = len(dist)
		}
		central := make([]string, 0, centralN)
		seen := map[string]struct{}{}
		for _, d := range dist {
			key := docs[d.idx].Key
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			central = append(central, key)
			if len(central) >= centralN {
				break
			}
		}

		out = append(out, MapCluster{
			ID:          id,
			Size:        len(members),
			TopKeywords: keywords,
			CentralKeys: central,
		})
	}

	return out
}

func sanitizeMapOptions(opts MapOptions) MapOptions {
	defaults := DefaultMapOptions()
	if opts.MaxIssues <= 0 {
		opts.MaxIssues = defaults.MaxIssues
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = defaults.MaxFiles
	}
	if opts.MaxFileNamesPerIssue <= 0 {
		opts.MaxFileNamesPerIssue = defaults.MaxFileNamesPerIssue
	}
	if opts.MaxDocChars <= 0 {
		opts.MaxDocChars = defaults.MaxDocChars
	}
	if opts.MaxVocab <= 0 {
		opts.MaxVocab = defaults.MaxVocab
	}
	if opts.MaxNeighbors <= 0 {
		opts.MaxNeighbors = defaults.MaxNeighbors
	}
	if opts.SimilarityDims <= 0 {
		opts.SimilarityDims = defaults.SimilarityDims
	}
	return opts
}

func (idx *Indexer) loadIssueFileNames(ctx context.Context, opts MapOptions) (map[string][]string, error) {
	limit := opts.MaxIssues * opts.MaxFileNamesPerIssue
	if limit <= 0 {
		return map[string][]string{}, nil
	}

	sql := fmt.Sprintf(`SELECT issue_key, file_name FROM %s ORDER BY updated_at DESC LIMIT %d`, filesTableName, limit)
	req := idx.client.UtilsAPI.Sql(ctx).Body(sql)
	resp, _, err := req.Execute()
	if err != nil {
		return nil, formatSQLError(err, sql)
	}

	result := make(map[string][]string)
	if resp.ArrayOfMapmapOfStringAny == nil {
		return result, nil
	}

	for _, queryResult := range *resp.ArrayOfMapmapOfStringAny {
		dataRows, ok := queryResult["data"].([]interface{})
		if !ok {
			continue
		}
		for _, rowRaw := range dataRows {
			rowMap, ok := rowRaw.(map[string]interface{})
			if !ok {
				continue
			}
			issueKey := getStringFromMap(rowMap, "issue_key")
			fileName := getStringFromMap(rowMap, "file_name")
			if issueKey == "" || fileName == "" {
				continue
			}
			if len(result[issueKey]) >= opts.MaxFileNamesPerIssue {
				continue
			}
			result[issueKey] = append(result[issueKey], fileName)
		}
	}

	return result, nil
}

func (idx *Indexer) loadIssueDocs(ctx context.Context, opts MapOptions, fileNames map[string][]string) ([]mapDocument, error) {
	sql := fmt.Sprintf(`SELECT id, issue_key, url, summary, description, comments_text FROM %s ORDER BY updated_at DESC LIMIT %d`, issuesTableName, opts.MaxIssues)
	req := idx.client.UtilsAPI.Sql(ctx).Body(sql)
	resp, _, err := req.Execute()
	if err != nil {
		return nil, formatSQLError(err, sql)
	}

	var docs []mapDocument
	if resp.ArrayOfMapmapOfStringAny == nil {
		return docs, nil
	}

	for _, queryResult := range *resp.ArrayOfMapmapOfStringAny {
		dataRows, ok := queryResult["data"].([]interface{})
		if !ok {
			continue
		}
		for _, rowRaw := range dataRows {
			rowMap, ok := rowRaw.(map[string]interface{})
			if !ok {
				continue
			}

			issueKey := getStringFromMap(rowMap, "issue_key")
			summary := getStringFromMap(rowMap, "summary")
			description := getStringFromMap(rowMap, "description")
			comments := getStringFromMap(rowMap, "comments_text")
			fileText := strings.Join(fileNames[issueKey], " ")

			text := strings.Join([]string{summary, description, comments, fileText}, " ")
			text = truncateText(text, opts.MaxDocChars)

			docs = append(docs, mapDocument{
				ID:    getStringFromMap(rowMap, "id"),
				Kind:  "issue",
				Key:   issueKey,
				Title: summary,
				URL:   getStringFromMap(rowMap, "url"),
				Text:  text,
			})
		}
	}

	return docs, nil
}

func (idx *Indexer) loadFileDocs(ctx context.Context, opts MapOptions) ([]mapDocument, error) {
	sql := fmt.Sprintf(`SELECT id, issue_key, issue_url, file_url, file_name, content_text, metadata_text FROM %s ORDER BY updated_at DESC LIMIT %d`, filesTableName, opts.MaxFiles)
	req := idx.client.UtilsAPI.Sql(ctx).Body(sql)
	resp, _, err := req.Execute()
	if err != nil {
		return nil, formatSQLError(err, sql)
	}

	var docs []mapDocument
	if resp.ArrayOfMapmapOfStringAny == nil {
		return docs, nil
	}

	for _, queryResult := range *resp.ArrayOfMapmapOfStringAny {
		dataRows, ok := queryResult["data"].([]interface{})
		if !ok {
			continue
		}
		for _, rowRaw := range dataRows {
			rowMap, ok := rowRaw.(map[string]interface{})
			if !ok {
				continue
			}

			fileName := getStringFromMap(rowMap, "file_name")
			content := truncateText(getStringFromMap(rowMap, "content_text"), opts.MaxDocChars)
			metadata := getStringFromMap(rowMap, "metadata_text")
			text := strings.Join([]string{fileName, metadata, content}, " ")
			text = truncateText(text, opts.MaxDocChars)

			docs = append(docs, mapDocument{
				ID:             getStringFromMap(rowMap, "id"),
				Kind:           "file",
				Key:            getStringFromMap(rowMap, "issue_key"),
				Title:          fileName,
				URL:            getStringFromMap(rowMap, "file_url"),
				ParentIssueURL: getStringFromMap(rowMap, "issue_url"),
				Text:           text,
			})
		}
	}

	return docs, nil
}

func buildTFIDFMatrix(docs []mapDocument, maxVocab int) ([]float64, []string, int, int, error) {
	if len(docs) == 0 {
		return nil, nil, 0, 0, nil
	}

	if maxVocab <= 0 {
		maxVocab = DefaultMapOptions().MaxVocab
	}

	perDocCounts := make([]map[string]int, len(docs))
	df := make(map[string]int)

	for i, doc := range docs {
		counts := make(map[string]int)
		for _, token := range tokenize(doc.Text) {
			counts[token]++
		}
		perDocCounts[i] = counts
		for token := range counts {
			df[token]++
		}
	}

	tokens := make([]tokenStat, 0, len(df))
	for token, count := range df {
		tokens = append(tokens, tokenStat{token: token, df: count})
	}

	sort.Slice(tokens, func(i, j int) bool {
		if tokens[i].df == tokens[j].df {
			return tokens[i].token < tokens[j].token
		}
		return tokens[i].df > tokens[j].df
	})

	if len(tokens) > maxVocab {
		tokens = tokens[:maxVocab]
	}

	vocabIndex := make(map[string]int, len(tokens))
	vocab := make([]string, len(tokens))
	idf := make([]float64, len(tokens))
	for i, token := range tokens {
		vocabIndex[token.token] = i
		vocab[i] = token.token
		idf[i] = math.Log((1.0+float64(len(docs)))/(1.0+float64(token.df))) + 1.0
	}

	rows := len(docs)
	cols := len(tokens)
	data := make([]float64, rows*cols)

	for i, counts := range perDocCounts {
		row := data[i*cols : (i+1)*cols]
		for token, count := range counts {
			idx, ok := vocabIndex[token]
			if !ok {
				continue
			}
			tf := 1.0 + math.Log(float64(count))
			row[idx] = tf * idf[idx]
		}
		normalizeVector(row)
	}

	return data, vocab, rows, cols, nil
}

type tokenStat struct {
	token string
	df    int
}

func tokenize(s string) []string {
	var tokens []string
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			tokens = append(tokens, b.String())
			b.Reset()
		}
	}
	if b.Len() > 0 {
		tokens = append(tokens, b.String())
	}
	return tokens
}

func truncateText(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func reduceWithSVD(data []float64, rows, cols, simDims int) ([][2]float64, [][]float64, error) {
	coords := make([][2]float64, rows)
	reduced := make([][]float64, rows)
	if rows == 0 || cols == 0 {
		return coords, reduced, nil
	}

	matrixData := make([]float64, len(data))
	copy(matrixData, data)
	matrix := mat.NewDense(rows, cols, matrixData)

	var svd mat.SVD
	if ok := svd.Factorize(matrix, mat.SVDThin); !ok {
		return nil, nil, fmt.Errorf("svd factorization failed")
	}

	values := svd.Values(nil)
	if len(values) == 0 {
		return coords, reduced, nil
	}

	if simDims <= 0 || simDims > len(values) {
		simDims = len(values)
	}

	var u mat.Dense
	svd.UTo(&u)

	for i := 0; i < rows; i++ {
		vec := make([]float64, simDims)
		for d := 0; d < simDims; d++ {
			vec[d] = u.At(i, d) * values[d]
		}
		reduced[i] = vec
	}

	if simDims == 1 {
		for i := range coords {
			coords[i] = [2]float64{reduced[i][0], 0}
		}
		return coords, reduced, nil
	}

	for i := range coords {
		coords[i] = [2]float64{reduced[i][0], reduced[i][1]}
	}

	return coords, reduced, nil
}

func normalizeVector(vec []float64) {
	var sum float64
	for _, v := range vec {
		sum += v * v
	}
	if sum == 0 {
		return
	}
	inv := 1.0 / math.Sqrt(sum)
	for i := range vec {
		vec[i] *= inv
	}
}

func cluster2D(points [][2]float64, k int) []int {
	assignments := make([]int, len(points))
	if len(points) == 0 {
		return assignments
	}

	if k <= 0 {
		k = int(math.Sqrt(float64(len(points))))
		if k < 2 {
			k = 2
		}
	}
	if k > len(points) {
		k = len(points)
	}

	centroids := initCentroids(points, k)
	for iter := 0; iter < 20; iter++ {
		changed := false
		for i, p := range points {
			best := 0
			bestDist := distance2(p, centroids[0])
			for c := 1; c < k; c++ {
				d := distance2(p, centroids[c])
				if d < bestDist {
					bestDist = d
					best = c
				}
			}
			if assignments[i] != best {
				assignments[i] = best
				changed = true
			}
		}

		centroids = recomputeCentroids(points, assignments, k)
		if !changed {
			break
		}
	}

	return assignments
}

func initCentroids(points [][2]float64, k int) [][2]float64 {
	centroids := make([][2]float64, k)
	if k == 0 {
		return centroids
	}
	step := len(points) / k
	if step == 0 {
		step = 1
	}
	for i := 0; i < k; i++ {
		idx := i * step
		if idx >= len(points) {
			idx = len(points) - 1
		}
		centroids[i] = points[idx]
	}
	return centroids
}

func recomputeCentroids(points [][2]float64, assignments []int, k int) [][2]float64 {
	centroids := make([][2]float64, k)
	counts := make([]int, k)
	for i, p := range points {
		c := assignments[i]
		centroids[c][0] += p[0]
		centroids[c][1] += p[1]
		counts[c]++
	}
	for i := 0; i < k; i++ {
		if counts[i] == 0 {
			continue
		}
		centroids[i][0] /= float64(counts[i])
		centroids[i][1] /= float64(counts[i])
	}
	return centroids
}

func distance2(a, b [2]float64) float64 {
	dx := a[0] - b[0]
	dy := a[1] - b[1]
	return dx*dx + dy*dy
}

func buildNeighbors(reduced [][]float64, docs []mapDocument, maxNeighbors int) [][]MapNeighbor {
	neighbors := make([][]MapNeighbor, len(docs))
	if len(reduced) == 0 || maxNeighbors <= 0 {
		return neighbors
	}

	normalized := make([][]float64, len(reduced))
	for i, vec := range reduced {
		copyVec := make([]float64, len(vec))
		copy(copyVec, vec)
		normalizeVector(copyVec)
		normalized[i] = copyVec
	}

	for i := range normalized {
		top := make([]MapNeighbor, 0, maxNeighbors)
		var minIdx int
		var minScore float64
		for j := range normalized {
			if i == j {
				continue
			}
			score := dot(normalized[i], normalized[j])
			candidate := MapNeighbor{ID: docs[j].ID, Score: score}
			if len(top) < maxNeighbors {
				top = append(top, candidate)
				if len(top) == 1 || score < minScore {
					minScore = score
					minIdx = len(top) - 1
				}
				continue
			}
			if score > minScore {
				top[minIdx] = candidate
				minScore = top[0].Score
				minIdx = 0
				for idx := 1; idx < len(top); idx++ {
					if top[idx].Score < minScore {
						minScore = top[idx].Score
						minIdx = idx
					}
				}
			}
		}
		sort.Slice(top, func(a, b int) bool {
			return top[a].Score > top[b].Score
		})
		neighbors[i] = top
	}

	return neighbors
}

func dot(a, b []float64) float64 {
	var sum float64
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

func uniqueClusterCount(assignments []int) int {
	seen := map[int]struct{}{}
	for _, v := range assignments {
		seen[v] = struct{}{}
	}
	return len(seen)
}
