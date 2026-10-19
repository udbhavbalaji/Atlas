'use strict';
const origin = document.getElementById('sync-origin');
const status = document.getElementById('sync-status');
const results = document.getElementById('sync-results');
origin.textContent = location.origin;

let lastRevision = null;
let checking = false;
async function checkServer(force = false) {
  if (checking || document.hidden) return;
  checking = true;
  if (force || lastRevision === null) status.textContent = 'Checking your Atlas service…';
  try {
    if (force || lastRevision === null) {
      const health = await fetch('/healthz', {cache: 'no-store'});
      if (!health.ok || (await health.json()).status !== 'ok') throw new Error('This Atlas service is unavailable.');
    }
    const revisionResponse = await fetch('/api/v1/records/revision', {cache: 'no-store'});
    if (!revisionResponse.ok) throw new Error('Could not check saved record changes.');
    const {revision} = await revisionResponse.json();
    if (!Number.isSafeInteger(revision)) throw new Error('Atlas sent an invalid records revision.');
    const changed = force || revision !== lastRevision;
    if (changed) {
      const paths = [['Tasks', '/api/v1/tasks'], ['Reminders', '/api/v1/reminders'],
        ['Notes', '/api/v1/notes'], ['Reports', '/api/v1/debug/reports']];
      const responses = await Promise.all(paths.map(([, path]) => fetch(path, {cache: 'no-store'})));
      const items = await Promise.all(responses.map(response => {
        if (!response.ok) throw new Error('Could not read saved items from this service.');
        return response.json();
      }));
      results.replaceChildren();
      paths.forEach(([label], index) => {
        const card = document.createElement('span');
        card.className = 'report-card';
        card.textContent = label + ': ' + items[index].length;
        results.append(card);
      });
      lastRevision = revision;
    }
    if (changed) status.textContent = 'Connected · records revision ' + revision + ' · watching for changes every second.';
  } catch (error) {
    status.textContent = error.message;
  } finally {
    checking = false;
  }
}
document.getElementById('sync-check').onclick = () => checkServer(true);
document.addEventListener('visibilitychange', () => { if (!document.hidden) checkServer(); });
setInterval(() => checkServer(), 1000);
checkServer(true);
