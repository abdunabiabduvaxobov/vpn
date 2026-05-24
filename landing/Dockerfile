# syntax=docker/dockerfile:1.7
#
# Multi-stage Dockerfile for the Rise VPN landing — produces the
# `landing-node` runner image used by Phase 4 D-02 hybrid routing.
#
# Stages:
#   deps    — pure npm ci, restored from cache when package.json is unchanged
#   builder — copies the workspace, sets build-arg placeholders for env vars
#             that env.ts validates at module load, then runs `next build`.
#             The standalone bundle is emitted to .next/standalone (D-01).
#   runner  — node:22-alpine, non-root user `nextjs`, copies the standalone
#             bundle + .next/static + public/, exposes 3000, runs server.js.
#
# Build args (NOT runtime secrets — these are placeholders that satisfy
# env.ts's fail-fast validation at build time. Runtime values come from
# compose's `${VAR:?}` env substitution.):
#   BACKEND_API_URL, REVALIDATE_SECRET, APPLE_SERVICE_ID,
#   APPLE_REDIRECT_URI, GOOGLE_CLIENT_ID_WEB, GOOGLE_REDIRECT_URI, APP_URL

FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json* ./
# Bump npm to match the host (>=11) so its lockfile resolver agrees with
# the lockfile produced locally. Alpine ships node:22 with npm 10 which
# refuses some transitive-peer overlaps that npm >=11 accepts.
RUN npm install -g npm@latest && npm ci

FROM node:22-alpine AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
# Build-time env placeholders. Real values are injected at container
# start via docker-compose.landing.yml's `${VAR:?}` substitutions.
ARG BACKEND_API_URL=https://placeholder
ARG REVALIDATE_SECRET=placeholder
ARG APPLE_SERVICE_ID=placeholder
ARG APPLE_REDIRECT_URI=https://placeholder/auth/callback?provider=apple
ARG GOOGLE_CLIENT_ID_WEB=placeholder
ARG GOOGLE_REDIRECT_URI=https://placeholder/auth/callback?provider=google
ARG APP_URL=https://placeholder
ENV BACKEND_API_URL=$BACKEND_API_URL \
    REVALIDATE_SECRET=$REVALIDATE_SECRET \
    APPLE_SERVICE_ID=$APPLE_SERVICE_ID \
    APPLE_REDIRECT_URI=$APPLE_REDIRECT_URI \
    GOOGLE_CLIENT_ID_WEB=$GOOGLE_CLIENT_ID_WEB \
    GOOGLE_REDIRECT_URI=$GOOGLE_REDIRECT_URI \
    APP_URL=$APP_URL
RUN npm run build

FROM node:22-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
# Drop privileges — addgroup/adduser BusyBox flags (no `--system` long-form;
# `-S` means system account, `-G` adds to group, `-D` skips password prompt).
RUN addgroup -S -g 1001 nodejs && adduser -S -u 1001 -G nodejs nextjs
COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
USER nextjs
EXPOSE 3000
ENV HOSTNAME=0.0.0.0 PORT=3000
CMD ["node", "server.js"]
