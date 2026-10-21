const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const vm = require('node:vm');

// Execute the production component with a controllable provider. Callbacks are
// deliberately delivered out of order to reproduce delayed SDK close events.
function checkout(options = {}) {
  const source = readFileSync(new URL('../static/js/app.js', `file://${__filename}`), 'utf8');
  const begin = source.indexOf("  Alpine.data('razorpayCheckout'");
  const end = source.indexOf('  // ── sign-in card', begin);
  assert(begin >= 0 && end > begin, 'checkout component must exist');
  let factory, requests = 0, providerOpen = false;
  const instances = [];
  class Provider {
    constructor(options) { this.options = options; this.events = {}; instances.push(this); }
    on(name, fn) { this.events[name] = fn; }
    open() { assert.equal(providerOpen, false, 'only one provider window may open'); providerOpen = true; }
    close() { providerOpen = false; this.options.modal.ondismiss(); }
    fail(message) { this.events['payment.failed']({ error: { description: message } }); }
  }
  const dialog = {
    open: false,
    showModal() { assert.equal(providerOpen, false, 'error must not cover an open provider'); this.open = true; },
    close() { this.open = false; },
  };
  const context = {
    AbortController, setTimeout: options.setTimeout || setTimeout, clearTimeout, setInterval, clearInterval,
    sessionStorage: options.storage || { getItem() {}, setItem() {}, removeItem() {} },
    Alpine: { data(_name, fn) { factory = fn; } },
    Razorpay: Provider,
    window: { Razorpay: Provider },
    fetch: options.fetch || (async (url) => {
      requests++;
      if (url.includes('verify-payment')) return { ok: false, json: async () => ({ error: 'Confirmation delayed' }) };
      return { ok: true, json: async () => ({ order_id: `order-${requests}`, amount: 5999900, currency: 'INR', key_id: 'rzp_test_fixture' }) };
    }),
    lwToast() {}, lwProcessing() {}, location: { assign() {} },
  };
  vm.runInNewContext(source.slice(begin, end), context);
  const data = factory(5999900);
  data.$refs = { paymentIssue: dialog };
  return { data, dialog, instances, requestCount: () => requests };
}

test('failure waits for provider dismissal and amount limits do not suggest an identical retry', async () => {
  const { data, dialog, instances } = checkout();
  await data.pay();
  const provider = instances[0];
  assert.equal(provider.options.retry.enabled, false);
  provider.fail('Amount exceeds maximum amount allowed.');
  provider.fail('Duplicate failure');
  data.syncFailureDialog();
  assert.equal(dialog.open, false);
  assert.equal(data.busy, true);
  provider.close();
  data.syncFailureDialog();
  assert.equal(dialog.open, true);
  assert.equal(data.amountLimited, true);
  assert.equal(data.error, 'Amount exceeds maximum amount allowed.');
  assert.match(data.failureHelp, /change the total/);
});

test('a retry closes the error first; callbacks from old attempts cannot reopen it', async () => {
  const { data, dialog, instances } = checkout();
  await data.pay();
  const old = instances[0];
  old.close(); data.syncFailureDialog();
  assert.equal(dialog.open, true);
  await data.pay();
  assert.equal(dialog.open, false);
  old.options.modal.ondismiss();
  old.fail('Delayed old failure');
  data.syncFailureDialog();
  assert.equal(data.paymentFailed, false);
  assert.equal(data.gatewayOpen, true);
  assert.equal(data.busy, true);
  assert.equal(instances.length, 2);
});

test('rapid clicks cannot create multiple checkout instances', async () => {
  const { data, instances, requestCount } = checkout();
  await Promise.all([data.pay(), data.pay(), data.pay()]);
  assert.equal(instances.length, 1);
  assert.equal(requestCount(), 1);
  await data.pay();
  assert.equal(instances.length, 1);
});

test('COD selection closes the popup without placing an order', async () => {
  const { data, dialog, instances, requestCount } = checkout();
  await data.pay(); instances[0].close(); data.syncFailureDialog();
  data.chooseCash();
  assert.equal(data.method, 'cod');
  assert.equal(dialog.open, false);
  assert.equal(requestCount(), 1);
});

test('successful payment callbacks suppress cancellation and retain confirmation retry', async () => {
  const { data, instances, requestCount } = checkout();
  await data.pay();
  const provider = instances[0];
  await provider.options.handler({ razorpay_payment_id: 'pay-fixture' });
  provider.options.modal.ondismiss();
  provider.fail('Late failure');
  assert.equal(data.paymentFailed, false);
  assert.match(data.error, /do not pay again/);
  await data.pay();
  assert.equal(instances.length, 1, 'confirmation retry must not open another payment');
  assert.equal(requestCount(), 3);
  assert.equal(data.pendingPayment.razorpay_payment_id, 'pay-fixture');
});


test('server cooldown prevents button spam and releases busy state', async () => {
  let requests = 0;
  const { data, instances } = checkout({ fetch: async () => {
    requests++;
    return { ok: false, headers: { get: () => '20' }, json: async () => ({ error: 'Too many payment requests' }) };
  }});
  await data.pay();
  assert.equal(data.cooldown, 20);
  assert.equal(data.busy, false);
  await data.pay();
  assert.equal(requests, 1);
  assert.equal(instances.length, 0);
  data.destroy();
});

test('captured payment recovered by server confirms without reopening provider', async () => {
  const calls = [];
  const payment = { razorpay_payment_id: 'pay-recovered', razorpay_order_id: 'order-recovered', razorpay_signature: 'signature' };
  const { data, instances } = checkout({ fetch: async (url) => {
    calls.push(url);
    if (url.includes('create-order')) return { ok: true, json: async () => ({ payment }) };
    return { ok: false, json: async () => ({ error: 'Confirmation delayed' }) };
  }});
  await data.pay();
  assert.equal(instances.length, 0);
  assert.equal(data.pendingPayment.razorpay_payment_id, 'pay-recovered');
  assert.deepEqual(calls, ['/api/create-order', '/api/verify-payment']);
  assert.equal(data.busy, false);
});

test('uncertain authorised payment blocks COD and another payment window', async () => {
  const { data, instances } = checkout({ fetch: async () => ({ ok: false, json: async () => ({ error: 'Earlier payment pending', payment_pending: true }) }) });
  await data.pay();
  data.chooseCash();
  assert.equal(data.method, 'razorpay');
  assert.equal(data.pendingAttempt, true);
  assert.equal(data.paymentFailed, false);
  assert.equal(instances.length, 0);
});

test('confirmation proof survives refresh within its recovery window', () => {
  const { data } = checkout({ storage: { getItem: () => JSON.stringify({ amount: 5999900, expires: Date.now() + 10000, payment: { razorpay_payment_id: 'pay-saved' } }) } });
  data.$watch = () => {};
  data.init();
  assert.equal(data.pendingPayment.razorpay_payment_id, 'pay-saved');
});


test('slow response aborts and releases checkout without opening a second payment', async () => {
  let expire;
  const { data, instances } = checkout({
    setTimeout: (fn) => { expire = fn; return undefined; },
    fetch: (_url, options) => new Promise((_resolve, reject) => {
      options.signal.addEventListener('abort', () => reject(Object.assign(new Error('Aborted'), { name: 'AbortError' })));
    }),
  });
  const started = data.pay();
  expire();
  await started;
  assert.equal(data.busy, false);
  assert.equal(instances.length, 0);
  assert.match(data.error, /same payment attempt/);
});
