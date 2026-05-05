FROM golang:1.26 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/ytbs ./

FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=builder /out/ytbs /usr/local/bin/ytbs

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/ytbs"]
CMD ["serve", "--addr", ":8080"]
