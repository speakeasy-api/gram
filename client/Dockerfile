FROM node:18 AS build

ARG NODE_ENV=production
ENV NODE_ENV=${NODE_ENV}

WORKDIR /app

# Copy workspace files and TypeScript configurations
COPY package.json .
COPY pnpm-lock.yaml .
COPY pnpm-workspace.yaml .
COPY tsconfig.json .
COPY tsconfig.strict.json .
COPY client/dashboard/package.json ./client/dashboard/
COPY client/sdk/package.json ./client/sdk/

# Install dependencies
RUN npm install -g pnpm
RUN pnpm install --frozen-lockfile

# Copy source files for SDK and dashboard
COPY client/sdk ./client/sdk
COPY client/dashboard ./client/dashboard

# Build the SDK first
WORKDIR /app/client/sdk
RUN pnpm build

# Build the dashboard
WORKDIR /app/client/dashboard
RUN pnpm build

# Stage 2: Production environment
FROM nginxinc/nginx-unprivileged:alpine

# Copy the build output to nginx
COPY --from=build /app/client/dashboard/dist /usr/share/nginx/html

# Copy custom nginx config
COPY --from=build /app/client/dashboard/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 3000

CMD ["nginx", "-g", "daemon off;"]