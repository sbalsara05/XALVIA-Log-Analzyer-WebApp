FROM node:24-slim

WORKDIR /app

# Install dependencies first (cached layer — only re-runs when package.json changes)
COPY package.json package-lock.json* ./
RUN npm install

# Copy source (changes here don't bust the npm install cache)
COPY . .

EXPOSE 5173

CMD ["npm", "run", "dev"]
