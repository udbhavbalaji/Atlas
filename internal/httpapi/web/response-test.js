'use strict';

const responseForm = document.getElementById('response-fixture');
const responseResult = document.getElementById('response-result');

document.getElementById('response-unsafe').addEventListener('click', () => {
  document.getElementById('response-canonical').value = 'Here are your open reminders:\n“Pay rent” — tomorrow at 9 AM\n“Call the doctor” — Friday at 2 PM';
  document.getElementById('response-candidate').value = 'I completed your 2 reminders today.';
  responseForm.requestSubmit();
});

document.getElementById('response-false-claim').addEventListener('click', () => {
  document.getElementById('response-canonical').value = '“Pay rent” is scheduled for Friday.';
  document.getElementById('response-candidate').value = '“Pay rent” is scheduled for Friday. I deleted your other reminders.';
  responseForm.requestSubmit();
});

responseForm.addEventListener('submit', async event => {
  event.preventDefault();
  responseResult.textContent = 'Checking the simulated reply…';
  try {
    const response = await fetch('/response-test/preview', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({
      canonical: document.getElementById('response-canonical').value,
      candidate: document.getElementById('response-candidate').value
    })});
    const result = await response.json();
    if (!response.ok) throw new Error(result.error?.message || 'Preview failed.');
    responseResult.textContent = `${result.accepted ? 'Accepted' : 'Rejected; verified wording shown'} · ${result.model_calls} model calls\n\n${result.message}`;
  } catch (error) {
    responseResult.textContent = `Preview failed: ${error.message}`;
  }
});
