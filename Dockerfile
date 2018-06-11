FROM golang:1.10 as builder

WORKDIR /go/src/github.com/rnaveiras/postgres_exporter

RUN go get github.com/golang/dep/cmd/dep
COPY . ./
# RUN dep ensure -v --vendor-only

ARG git_revision=unset
ARG version=unset
ENV CGO_ENABLED=0 GOOS=linux
ENV REPO=github.com/rnaveiras/postgres_exporter

RUN set -x \
      && go build -o postgres_exporter -ldflags \
      "-X ${REPO}/vendor/github.com/prometheus/common/version.Version=${version} \
      -X ${REPO}/vendor/github.com/prometheus/common/version.Revision=${git_revision} \
      -X ${REPO}/vendor/github.com/prometheus/common/version.Branch=master \
      -X ${REPO}/vendor/github.com/prometheus/common/version.BuildUser=raul \
      -X ${REPO}/vendor/github.com/prometheus/common/version.BuildDate=$(date -u +"%Y%m%d-%H:%m:%S")" \
      -a -tags netgo ${REPO}

FROM alpine:3.7
RUN apk --no-cache --update add ca-certificates

COPY --from=builder /go/src/github.com/rnaveiras/postgres_exporter/postgres_exporter /bin/postgres_exporter

USER nobody
ENTRYPOINT ["/bin/postgres_exporter"]
