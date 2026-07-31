# syntax=docker/dockerfile:1

# ---- Build stage -----------------------------------------------------------
FROM node:20-alpine AS build
WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm ci

COPY . .

# VITE_API_BASE_URL is baked into the JS bundle at build time. Default to a
# same-origin relative path so the nginx reverse proxy below (which forwards
# /api/ to the backend) works out of the box; override with --build-arg for
# a different deployment topology (e.g. a separate API domain).
ARG VITE_API_BASE_URL=/api/v1
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}

RUN npm run build

# ---- Runtime stage ----------------------------------------------------------
FROM nginx:1.27-alpine AS runtime

RUN rm -rf /usr/share/nginx/html/*
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf.template /etc/nginx/templates/default.conf.template

# The backend control-plane upstream nginx proxies /api/ to. Point this at
# the backend Service/host reachable from this container, e.g.
# "http://backend:8080" in docker-compose or
# "http://backend.nexusvpn.svc.cluster.local:8080" in Kubernetes.
ENV API_UPSTREAM=http://backend:8080

EXPOSE 8080

# envsubst renders /etc/nginx/templates/*.template -> /etc/nginx/conf.d/*.conf
# on container start (built into the nginx:*-alpine image entrypoint), so
# ${API_UPSTREAM} above is substituted with the real value at runtime.
CMD ["nginx", "-g", "daemon off;"]
