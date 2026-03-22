FROM mcr.microsoft.com/playwright:v1.52.0-noble

WORKDIR /app
COPY package.json .
RUN npm install && npx playwright install chromium
COPY server.js .

EXPOSE 3100
CMD ["node", "server.js"]
