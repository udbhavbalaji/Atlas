'use strict';
let fixtureConversation = null;
const status = document.getElementById('report-test-status');
const fixture = document.getElementById('report-fixture');
const openReport = document.getElementById('report-open');
const list = document.getElementById('report-list');

fixture.onclick = async () => {
  fixture.disabled = true;
  status.textContent = 'Starting a local fixture…';
  try {
    const response = await fetch('/api/v1/conversations/routing', {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({provider: 'mock', fixture: 'unsupported', version: '1',
        request_id: crypto.randomUUID(), text: 'Send an email',
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'})
    });
    const body = await response.json();
    if (!response.ok) throw new Error(body.error?.message || 'Could not start the fixture.');
    fixtureConversation = body;
    openReport.disabled = false;
    status.textContent = 'Fixture ready. Atlas classified the request as unsupported.';
    const output = document.getElementById('report-test-conversation');
    output.replaceChildren();
    for (const turn of body.messages || []) {
      const p = document.createElement('p');
      p.textContent = (turn.role === 'user' ? 'You: ' : 'Atlas: ') + turn.text;
      output.append(p);
    }
  } catch (error) { status.textContent = error.message; }
  finally { fixture.disabled = false; }
};
openReport.onclick = () => AtlasReport.open(fixtureConversation);

async function loadReports() {
  list.textContent = 'Loading reports…';
  try {
    const response = await fetch('/api/v1/debug/reports');
    const reports = await response.json();
    if (!response.ok) throw new Error(reports.error?.message || 'Could not load reports.');
    list.replaceChildren();
    if (!reports.length) {
      const empty = document.createElement('p');
      empty.className = 'empty';
      empty.textContent = 'No reports yet. Report a conversation to build your candidate corpus.';
      list.append(empty);
      return;
    }
    for (const report of reports) {
      const card = document.createElement('article');
      card.className = 'report-card';
      const heading = document.createElement('div');
      heading.className = 'conversation-heading';
      const title = document.createElement('strong');
      title.textContent = report.category.replaceAll('_', ' ');
      const meta = document.createElement('span');
      meta.className = 'meta';
      meta.textContent = new Date(report.created_at).toLocaleString() + ' · ' + report.surface;
      heading.append(title, meta);
      const expected = document.createElement('p');
      expected.textContent = 'Expected: ' + report.expected;
      const details = document.createElement('details');
      const summary = document.createElement('summary');
      summary.textContent = 'Review conversation snapshot';
      const transcript = document.createElement('div');
      for (const turn of report.snapshot?.messages || []) {
        const line = document.createElement('p');
        line.textContent = (turn.role === 'user' ? 'You: ' : 'Atlas: ') + turn.text;
        transcript.append(line);
      }
      const route = document.createElement('p');
      route.className = 'meta';
      route.textContent = `Observed: ${report.snapshot?.state || 'unknown'} · ${report.snapshot?.provider || 'unknown'} · session ${report.session_id} v${report.session_version}`;
      details.append(summary, route, transcript);
      card.append(heading, expected, details);
      list.append(card);
    }
  } catch (error) { list.textContent = error.message; }
}
document.getElementById('report-refresh').onclick = loadReports;
window.addEventListener('atlas-report-saved', loadReports);
loadReports();
