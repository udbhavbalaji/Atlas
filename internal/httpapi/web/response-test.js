'use strict';

const responseForm = document.getElementById('response-fixture');
const responseResult = document.getElementById('response-result');

document.getElementById('response-unsafe').addEventListener('click', () => {
  document.getElementById('response-candidate').value = 'I completed your 2 reminders today.';
  responseForm.requestSubmit();
});

responseForm.addEventListener('submit', async event => {
  event.preventDefault();
  responseResult.textContent = 'Checking the simulated reply…';
  try {
    const response = await fetch('/response-test/preview', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({
      canonical: document.getElementById('response-canonical').value,
      candidate: document.getElementById('response-candidate').value,
      titles: document.getElementById('response-titles').value.split('\n').map(title => title.trim()).filter(Boolean)
    })});
    const result = await response.json();
    if (!response.ok) throw new Error(result.error?.message || 'Preview failed.');
    responseResult.textContent = `${result.accepted ? 'Accepted' : 'Rejected; verified wording shown'} · ${result.model_calls} model calls\n\n${result.message}`;
  } catch (error) {
    responseResult.textContent = `Preview failed: ${error.message}`;
  }
});
