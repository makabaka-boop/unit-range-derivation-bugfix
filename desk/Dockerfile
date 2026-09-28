# syntax=docker/dockerfile:1
# ---- 构建阶段 ----
FROM node:20-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY . .
RUN npm run build

# ---- 运行阶段：静态资源由 vite preview 提供，/api 由同 compose 的 units 提供 ----
FROM node:20-alpine
WORKDIR /app
RUN adduser -D -u 10001 appuser
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
COPY --from=build /app/package.json ./package.json
COPY --from=build /app/vite.config.js ./vite.config.js
USER appuser
EXPOSE 5173
ENV NODE_ENV=production
CMD ["npm", "run", "preview", "--", "--host", "0.0.0.0", "--port", "5173"]
