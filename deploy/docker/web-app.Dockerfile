FROM node:26.5.0-alpine AS build

WORKDIR /workspace
COPY package.json package-lock.json ./
COPY apps/web/package.json apps/web/package.json
COPY apps/admin-web/package.json apps/admin-web/package.json
RUN npm ci

COPY apps ./apps
ARG WORKSPACE=@cytisus/web
RUN npm run build --workspace=${WORKSPACE}

FROM node:26.5.0-alpine
WORKDIR /workspace
ENV NODE_ENV=production
COPY --from=build /workspace /workspace
ARG WORKSPACE=@cytisus/web
ENV WORKSPACE=${WORKSPACE}
USER node
CMD ["sh", "-c", "npm run start --workspace=${WORKSPACE}"]
