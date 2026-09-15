FROM docker.m.daocloud.io/library/golang:1.22-alpine AS build
WORKDIR /src
RUN go env -w GOPROXY=https://goproxy.cn,direct
COPY go.mod ./
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server

FROM docker.m.daocloud.io/library/alpine:3.20
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=build /out/server /app/server
COPY configs/mapping.yaml /app/configs/mapping.yaml
ENV HTTP_ADDR=:8080
ENV MAPPING_FILE=/data/mapping.yaml
ENV MAPPING_DEFAULT=/app/configs/mapping.yaml
EXPOSE 8080
CMD ["/app/server"]
