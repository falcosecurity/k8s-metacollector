# Build the manager binary
FROM golang:1.21.1 as builder

ARG RELEASE
ARG COMMIT
ARG BUILD_DATE
ARG PROJECT=github.com/falcosecurity/k8s-metacollector

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY . ./

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags "-s -w" -o meta-collector main.go
RUN CGO_ENABLED=0 \
    GOOS=$(go env GOOS) \
    GOARCH=$(go env GOARCH) \
    go build -ldflags \
    "-s \
    -w \
    -X '${PROJECT}/pkg/version.semVersion=${RELEASE}' \
    -X '${PROJECT}/pkg/version.gitCommit=${COMMIT}' \
    -X '${PROJECT}/pkg/version.buildDate=${BUILD_DATE}'" \
    -o meta-collector main.go
# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/meta-collector .
USER 65532:65532

ENTRYPOINT ["/meta-collector"]
