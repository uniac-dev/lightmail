FROM node:22-alpine AS assets
WORKDIR /src
COPY package.json package-lock.json ./
COPY dashboard/package.json dashboard/package.json
COPY sdk/package.json sdk/package.json
RUN npm ci
COPY dashboard dashboard
COPY sdk sdk
RUN npm run build --workspace dashboard

FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY dashboard/server ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /dashboard .

FROM gcr.io/distroless/static-debian12:nonroot
COPY LICENSE THIRD_PARTY_NOTICES.md /licenses/
COPY --from=build /dashboard /dashboard
COPY --from=assets /src/dashboard/dist /srv
ENV DASHBOARD_ROOT=/srv DASHBOARD_ADDRESS=:8080
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/dashboard"]
