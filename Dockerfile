FROM golang:1.26.3 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/cabure ./cmd

FROM alpine:3.20
RUN apk add --no-cache ca-certificates git openssh-client && \
    addgroup -g 65532 -S nonroot && \
    adduser -S -D -u 65532 -G nonroot nonroot
WORKDIR /
COPY --from=build /out/cabure /cabure
USER 65532:65532
ENTRYPOINT ["/cabure"]
