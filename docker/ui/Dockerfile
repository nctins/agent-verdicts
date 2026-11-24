# Dev only for now: next dev; source arrives via bind mount (see compose.yaml).
FROM node:26.13.1-alpine AS dev
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
CMD ["npm", "run", "dev", "--", "--hostname", "0.0.0.0", "--port", "3000"]

FROM node:26.13.1-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# nginx.conf comes from the `dockerui` named context (docker/ui).
FROM nginxinc/nginx-unprivileged:1.31.7-alpine AS production
COPY --from=build /app/out /usr/share/nginx/html
COPY --from=dockerui nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
