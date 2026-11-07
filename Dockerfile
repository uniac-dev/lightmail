FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /lightmail ./cmd/lightmail
RUN mkdir /data && touch /data/.keep

FROM gcr.io/distroless/static-debian12:nonroot
COPY LICENSE THIRD_PARTY_NOTICES.md /licenses/
COPY --from=build /lightmail /lightmail
COPY --from=build --chown=65532:65532 /data /data
ENV SENDER_ADDRESS=:8080 SENDER_DATA_DIR=/data/sender
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/lightmail"]
