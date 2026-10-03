'use strict';

// Transport and state rules shared by the browser prototype and scripted tests.
class AtlasVoiceSession {
  constructor(fetcher, onState = () => {}) {
    this.fetcher = fetcher;
    this.onState = onState;
    this.state = null;
    this.base = '/api/v1/conversations/routing';
  }

  async request(path, method, body) {
    const response = await this.fetcher(this.base + path, {
      method,
      headers: body ? {'Content-Type': 'application/json'} : {},
      body: body ? JSON.stringify(body) : undefined
    });
    let result;
    try { result = await response.json(); }
    catch { throw new Error('Atlas returned a response that could not be read. Resume the session before retrying.'); }
    if (!response.ok) {
      const error = new Error(result.error?.message || 'Conversation request failed.');
      error.status = response.status;
      error.code = result.error?.code;
      throw error;
    }
    this.state = result;
    this.onState(result);
    return result;
  }

  start({provider, fixture, text, timezone, contextQuery = '', requestID}) {
    const body = {provider, version: '1', request_id: requestID, text, timezone, context_query: contextQuery};
    if (provider === 'mock') body.fixture = fixture;
    return this.request('', 'POST', body);
  }

  resume(id) {
    if (!id) throw new Error('Enter a session ID to resume.');
    return this.request('/' + encodeURIComponent(id), 'GET');
  }

  requireActive() {
    if (!this.state?.id) throw new Error('Start or resume a conversation first.');
    if (['cancelled', 'unsupported'].includes(this.state.state)) throw new Error('This conversation is closed. Start a new one.');
  }

  async reply(text, field = '', value = '') {
    this.requireActive();
    if (this.state.state === 'confirming') throw new Error('Confirmation is pending. Use Retry confirmation.');
    if (['awaiting_confirmation', 'awaiting_delete_confirmation'].includes(this.state.state) && /^(yes|yes save it|save it|confirm|confirm and save)[.! ]*$/i.test(text.trim())) {
      throw new Error('Use the confirmation button after reviewing the exact change.');
    }
    if (!text.trim() && !value.trim()) throw new Error('Say or type a reply first.');
    return this.request('/' + encodeURIComponent(this.state.id) + '/reply', 'POST', {
      version: this.state.version, text, field, value
    });
  }

  confirm() {
    this.requireActive();
    if (!['awaiting_confirmation', 'confirming', 'awaiting_delete_confirmation', 'deleting'].includes(this.state.state)) throw new Error('Review the pending action before confirming.');
    return this.request('/' + encodeURIComponent(this.state.id) + '/reply', 'POST', {
      version: this.state.version, text: 'yes'
    });
  }

  cancel() {
    this.requireActive();
    if (this.state.state === 'confirming') throw new Error('Confirmation is pending. Resume to learn the saved result.');
    return this.request('/' + encodeURIComponent(this.state.id) + '/reply', 'POST', {
      version: this.state.version, text: 'cancel'
    });
  }
}

if (typeof window !== 'undefined') window.AtlasVoiceSession = AtlasVoiceSession;
if (typeof module !== 'undefined') module.exports = {AtlasVoiceSession};
