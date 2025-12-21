FROM node:20-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci
COPY . .
# Build static assets (uses VITE_API_BASE from build args if provided)
ARG VITE_API_BASE=/api
ENV VITE_API_BASE=${VITE_API_BASE}
RUN npm run build

FROM node:20-alpine
WORKDIR /app
RUN npm install -g serve
COPY --from=build /app/dist ./dist
ENV PORT=5173
EXPOSE 5173
# Serve the built assets
CMD ["serve", "-s", "dist", "-l", "5173"]
