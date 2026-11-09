FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/calc-api ./cmd/api && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/calc-sync ./cmd/sync

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D app
WORKDIR /app
# cache dos zips CVM (volume montado aqui); dono precisa ser o usuário da app
RUN mkdir -p /app/data && chown -R app:app /app
USER app
COPY --from=build /bin/calc-api /bin/calc-sync /bin/
EXPOSE 8080
ENTRYPOINT ["/bin/calc-api"]
