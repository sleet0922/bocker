const fs = require('node:fs');

const message = fs.readFileSync('/etc/node-mount-message', 'utf8').trim();
fs.writeFileSync('/var/lib/node-mount-app/result.txt', 'node-mounted\n');
console.log(JSON.stringify({language: 'node', message}));
