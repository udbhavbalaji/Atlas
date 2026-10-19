'use strict';
const origin = document.getElementById('sync-origin');
const status = document.getElementById('sync-status');
const results = document.getElementById('sync-results');
origin.textContent = location.origin;

document.getElementById('sync-check').onclick = async () => {
  status.textContent = 'Checking your Atlas service…';
  results.replaceChildren();
  try {
    const paths = [['Tasks', '/api/v1/tasks'], ['Reminders', '/api/v1/reminders'],
      ['Notes', '/api/v1/notes'], ['Reports', '/api/v1/debug/reports']];
    const health = await fetch('/healthz');
    if (!health.ok || (await health.json()).status !== 'ok') throw new Error('This Atlas service is unavailable.');
    for (const [label, path] of paths) {
      const response = await fetch(path);
      if (!response.ok) throw new Error('Could not read ' + label.toLowerCase() + ' from this service.');
      const items = await response.json();
      const card = document.createElement('span');
      card.className = 'report-card';
      card.textContent = label + ': ' + items.length;
      results.append(card);
    }
    status.textContent = 'Connected to this Atlas service. Compare the address and counts on your other devices.';
  } catch (error) {
    status.textContent = error.message;
  }
};
