const canvas = document.getElementById('map-canvas');
const ctx = canvas.getContext('2d');
const statsEl = document.getElementById('stats');
const loadingEl = document.getElementById('map-loading');
const selectedEl = document.getElementById('selected-card');
const neighborsEl = document.getElementById('neighbors');
const searchInput = document.getElementById('search-input');
const legendEl = document.getElementById('legend');

const palette = [
    '#1a73e8', '#e37400', '#1e8e3e', '#d93025', '#9334e6',
    '#188038', '#c5221f', '#5f6368', '#0b8043', '#f9ab00'
];

let mapData = null;
let points = [];
let pointIndex = {};
let bounds = null;
let zoom = 1;
let pan = { x: 0, y: 0 };
let isDragging = false;
let dragStart = { x: 0, y: 0 };
let dragMoved = false;
let baseScale = 1;
const padding = 24;
let selectedId = null;
let searchTerm = '';

function resizeCanvas() {
    const rect = canvas.parentElement.getBoundingClientRect();
    canvas.width = rect.width;
    canvas.height = rect.height;
    updateBaseScale();
    render();
}

window.addEventListener('resize', resizeCanvas);

function fetchMap() {
    fetch('/api/map')
        .then(resp => resp.json())
        .then(data => {
            mapData = data;
            points = data.points || [];
            pointIndex = {};
            points.forEach(p => {
                pointIndex[p.id] = p;
            });
            bounds = computeBounds(points);
            normalizePoints();
            zoom = 1;
            pan = { x: 0, y: 0 };
            updateStats(data);
            updateLegend(data);
            loadingEl.style.display = 'none';
            resizeCanvas();
        })
        .catch(err => {
            loadingEl.textContent = 'Ошибка загрузки карты';
            console.error(err);
        });
}

function computeBounds(points) {
    const xs = points.map(p => p.x);
    const ys = points.map(p => p.y);
    const minX = Math.min(...xs, 0);
    const maxX = Math.max(...xs, 1);
    const minY = Math.min(...ys, 0);
    const maxY = Math.max(...ys, 1);
    return { minX, maxX, minY, maxY };
}

function normalizePoints() {
    if (!bounds) return;
    const rangeX = bounds.maxX - bounds.minX || 1;
    const rangeY = bounds.maxY - bounds.minY || 1;
    points.forEach(p => {
        p._nx = (p.x - bounds.minX) / rangeX;
        p._ny = (p.y - bounds.minY) / rangeY;
    });
}

function updateBaseScale() {
    baseScale = Math.max(1, Math.min(canvas.width, canvas.height) - padding * 2);
}

function toScreen(p) {
    const centerX = canvas.width / 2;
    const centerY = canvas.height / 2;
    const x = centerX + pan.x + (p._nx - 0.5) * baseScale * zoom;
    const y = centerY + pan.y + (p._ny - 0.5) * baseScale * zoom;
    return { x, y };
}

function screenToNormalized(x, y) {
    const centerX = canvas.width / 2;
    const centerY = canvas.height / 2;
    const nx = ((x - centerX - pan.x) / (baseScale * zoom)) + 0.5;
    const ny = ((y - centerY - pan.y) / (baseScale * zoom)) + 0.5;
    return { nx, ny };
}

function render() {
    if (!points.length) return;
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    points.forEach(p => {
        const pos = toScreen(p);
        const highlight = selectedId === p.id;
        const isMatch = searchTerm && matchesSearch(p, searchTerm);
        const radius = highlight ? 6 : isMatch ? 5 : 3;
        const color = palette[p.cluster % palette.length] || '#666';
        ctx.beginPath();
        ctx.arc(pos.x, pos.y, radius, 0, Math.PI * 2);
        ctx.fillStyle = highlight ? '#000' : color;
        ctx.globalAlpha = isMatch || !searchTerm ? 0.9 : 0.2;
        ctx.fill();
    });
    ctx.globalAlpha = 1;
}

function updateStats(data) {
    const stats = data.stats || {};
    statsEl.replaceChildren();
    [['Задач: ', stats.issues], ['Файлов: ', stats.files],
     ['Точек: ', stats.points], ['Кластеров: ', stats.clusters]].forEach(([label, value]) => {
        const row = document.createElement('div');
        row.appendChild(document.createTextNode(label));
        const strong = document.createElement('strong');
        strong.textContent = Number(value) || 0;
        row.appendChild(strong);
        statsEl.appendChild(row);
    });
}

function updateLegend(data) {
    const clusters = data.stats ? data.stats.clusters : 0;
    legendEl.innerHTML = '';
    for (let i = 0; i < clusters; i++) {
        const entry = document.createElement('span');
        const dot = document.createElement('i');
        dot.style.background = palette[i % palette.length];
        entry.appendChild(dot);
        entry.appendChild(document.createTextNode(`Кластер ${i + 1}`));
        legendEl.appendChild(entry);
    }
}

// Заголовки, ключи и ссылки приходят из трекера, поэтому собираются узлами
// через textContent, а не склейкой разметки: строка вида
// "<img src=x onerror=...>" в заголовке задачи иначе выполнилась бы.
function textRow(label, value) {
    const row = document.createElement('div');
    if (label) row.appendChild(document.createTextNode(label));
    row.appendChild(document.createTextNode(value));
    return row;
}

// safeURL пропускает только http(s): file_url приходит из трекера,
// а javascript:-ссылка в href сработала бы по клику.
function safeURL(raw) {
    try {
        const u = new URL(raw, window.location.origin);
        return (u.protocol === 'http:' || u.protocol === 'https:') ? u.href : null;
    } catch (e) {
        return null;
    }
}

function linkRow(label, href, text) {
    const row = document.createElement('div');
    const url = safeURL(href);
    if (!url) {
        row.textContent = text;
        return row;
    }
    const a = document.createElement('a');
    a.href = url;
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
    a.textContent = text;
    if (label) row.appendChild(document.createTextNode(label));
    row.appendChild(a);
    return row;
}

function setSelected(point) {
    if (!point) return;
    selectedId = point.id;

    selectedEl.replaceChildren();
    const title = document.createElement('div');
    const strong = document.createElement('strong');
    strong.textContent = point.title || point.key || '';
    title.appendChild(strong);
    selectedEl.appendChild(title);
    selectedEl.appendChild(textRow('Тип: ', point.kind === 'file' ? 'Файл' : 'Задача'));
    selectedEl.appendChild(textRow('Ключ: ', point.key || '-'));
    selectedEl.appendChild(linkRow('', point.url, 'Открыть'));
    if (point.parent_issue_url) {
        selectedEl.appendChild(linkRow('', point.parent_issue_url, 'Родительская задача'));
    }

    neighborsEl.replaceChildren();
    if (!point.neighbors || point.neighbors.length === 0) {
        neighborsEl.textContent = '-';
    } else {
        point.neighbors.forEach(n => {
            const neighbor = pointIndex[n.id];
            if (!neighbor) return;
            const item = document.createElement('div');
            item.className = 'neighbor-item';

            const link = linkRow('', neighbor.url, neighbor.title || neighbor.key || '');
            const score = document.createElement('span');
            score.className = 'neighbor-score';
            score.textContent = n.score.toFixed(2);
            item.appendChild(link);
            item.appendChild(score);

            item.addEventListener('click', () => {
                setSelected(neighbor);
                render();
            });
            neighborsEl.appendChild(item);
        });
    }
    render();
}

function matchesSearch(point, term) {
    const haystack = `${point.key || ''} ${point.title || ''}`.toLowerCase();
    return haystack.includes(term.toLowerCase());
}

function pickPointAt(x, y) {
    if (!points.length) return null;
    let best = null;
    let bestDist = 12;
    points.forEach(p => {
        const pos = toScreen(p);
        const dx = pos.x - x;
        const dy = pos.y - y;
        const dist = Math.sqrt(dx * dx + dy * dy);
        if (dist < bestDist) {
            bestDist = dist;
            best = p;
        }
    });
    return best;
}

canvas.addEventListener('mousedown', (event) => {
    isDragging = true;
    dragMoved = false;
    dragStart = { x: event.clientX, y: event.clientY };
    canvas.style.cursor = 'grabbing';
});

canvas.addEventListener('mousemove', (event) => {
    if (!isDragging) return;
    const dx = event.clientX - dragStart.x;
    const dy = event.clientY - dragStart.y;
    if (Math.abs(dx) > 0 || Math.abs(dy) > 0) {
        dragMoved = true;
    }
    pan.x += dx;
    pan.y += dy;
    dragStart = { x: event.clientX, y: event.clientY };
    render();
});

canvas.addEventListener('mouseup', (event) => {
    if (!isDragging) return;
    isDragging = false;
    canvas.style.cursor = 'grab';
    if (!dragMoved) {
        const rect = canvas.getBoundingClientRect();
        const x = event.clientX - rect.left;
        const y = event.clientY - rect.top;
        const best = pickPointAt(x, y);
        if (best) {
            setSelected(best);
        }
    }
});

canvas.addEventListener('mouseleave', () => {
    if (isDragging) {
        isDragging = false;
        canvas.style.cursor = 'grab';
    }
});

canvas.addEventListener('wheel', (event) => {
    event.preventDefault();
    const rect = canvas.getBoundingClientRect();
    const x = event.clientX - rect.left;
    const y = event.clientY - rect.top;

    const before = screenToNormalized(x, y);
    const factor = event.deltaY > 0 ? 0.9 : 1.1;
    const nextZoom = Math.min(6, Math.max(0.4, zoom * factor));
    zoom = nextZoom;

    const centerX = canvas.width / 2;
    const centerY = canvas.height / 2;
    pan.x = x - centerX - (before.nx - 0.5) * baseScale * zoom;
    pan.y = y - centerY - (before.ny - 0.5) * baseScale * zoom;

    render();
}, { passive: false });

searchInput.addEventListener('input', (event) => {
    searchTerm = event.target.value.trim();
    render();
});

fetchMap();
