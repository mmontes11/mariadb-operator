FROM golang:1.23.6-alpine3.20 AS builder

ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH}

WORKDIR /app

COPY go.mod go.sum /app/
RUN go mod download

COPY . /app
RUN go build -o mariadb-operator cmd/controller/*.go

FROM gcr.io/distroless/static AS app

WORKDIR /
COPY --from=builder /app/mariadb-operator /bin/mariadb-operator 
USER 65532:65532

ENTRYPOINT ["/bin/mariadb-operator"]
