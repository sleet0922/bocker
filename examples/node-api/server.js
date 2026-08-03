const express = require('express');
const fs = require('fs');

const configPath = process.env.APP_CONFIG || '/etc/node-api/config.json';
const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
config.service = config.service || {};
if (process.env.APP_MESSAGE) config.service.message = process.env.APP_MESSAGE;
if (process.env.APP_VERSION) config.service.version = process.env.APP_VERSION;

const app = express();
app.disable('x-powered-by');
app.use(express.json({ limit: `${config.limits.maxBodyBytes || 1048576}b` }));
app.get('/healthz', (_req, res) => res.json({ status: 'ok', language: 'node' }));
app.get('/config', (_req, res) => res.json(config));
app.get('/', (_req, res) => res.type('text').send(`${config.service.message} (${config.service.version})\n`));

const server = app.listen(process.env.APP_PORT || 3000, process.env.APP_HOST || '::', () => {
  console.log(`${config.service.name} listening on ${server.address().port}`);
});
const shutdown = () => server.close(() => process.exit(0));
process.on('SIGTERM', shutdown);
process.on('SIGINT', shutdown);
