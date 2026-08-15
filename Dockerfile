FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG SERVICE=api
RUN CGO_ENABLED=0 go build -o /out/service ./cmd/${SERVICE}

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/service /app/service
EXPOSE 8080
ENTRYPOINT ["/app/service"]
