import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import * as Tabs from '@radix-ui/react-tabs';

// Test-only listener accounting verifies actual removal, independently from
// whether an installed listener has become an inert early return.
const add = EventTarget.prototype.addEventListener;
const remove = EventTarget.prototype.removeEventListener;
const tracked = new Map();
const captureOf = (options) => typeof options === 'boolean' ? options : !!options?.capture;
EventTarget.prototype.addEventListener = function (type, fn, options) {
  const key = `${type}:${captureOf(options)}`;
  if (!tracked.has(this)) tracked.set(this, new Map());
  const kinds = tracked.get(this);
  if (!kinds.has(key)) kinds.set(key, new Set());
  kinds.get(key).add(fn);
  return add.call(this, type, fn, options);
};
EventTarget.prototype.removeEventListener = function (type, fn, options) {
  tracked.get(this)?.get(`${type}:${captureOf(options)}`)?.delete(fn);
  return remove.call(this, type, fn, options);
};
window.inputListenerCount = () => [...tracked.values()].reduce((sum, kinds) =>
  sum + [...kinds.values()].reduce((n, fns) => n + fns.size, 0), 0);
window.fixtureReceipt = { delivered: 0, blocked: 0, prevented: 0, key: 0 };
window.addEventListener('click', (event) => {
  if (event.target.closest?.('#immediate')) event.stopImmediatePropagation();
}, true);
window.PRIVATE_STATE_CANARY = 'InputTracePrivateStateCanary_9921';
function App() {
  const [tab, setTab] = useState('one');
  const [receipt, setReceipt] = useState({ ...window.fixtureReceipt });
  const mark = (key) => { window.fixtureReceipt[key]++; setReceipt({ ...window.fixtureReceipt }); };
  return <main>
    <h1>Input event boundaries</h1>
    <Tabs.Root value={tab} activationMode="manual" onValueChange={setTab}>
      <Tabs.List><Tabs.Trigger value="one">One</Tabs.Trigger>
        <Tabs.Trigger id="radix-two" value="two">Two</Tabs.Trigger></Tabs.List>
    </Tabs.Root>
    <section id="capture-blocker" onMouseDownCapture={(event) => event.stopPropagation()}>
      <button id="blocked" onMouseDown={() => mark('blocked')}>Blocked press</button>
    </section>
    <button id="prevented" onMouseDown={(event) => { event.preventDefault(); mark('prevented'); }}>Prevent default</button>
    <button id="delivered" onClick={function PrivateFunctionSourceCanary_3982() { mark('delivered'); }}>Delivered callback</button>
    <button id="immediate" onClick={() => mark('delivered')}>Earlier immediate stop</button>
    <button id="key" onKeyDown={(event) => { event.preventDefault(); event.stopPropagation(); mark('key'); }}>Activation key</button>
    <button id="navigate" onClick={() => { location.href = '/next'; }}>Navigate</button>
    <input id="private" type="password" defaultValue="InputTracePrivateValueCanary_6371"
      aria-label="InputTracePrivateLabelCanary_2481" data-testid="InputTracePrivateAttributeCanary_1732" />
    <div id="covered" style={{ position: 'relative', width: 180, height: 48, margin: 8 }}>
      <button id="covered-target" style={{ position: 'absolute', inset: 0 }}>Covered target</button>
      <button id="overlay" style={{ position: 'absolute', inset: 0 }}>Actual overlay</button>
    </div>
    <output>{JSON.stringify({ ...receipt, tab })}</output>
  </main>;
}
createRoot(document.getElementById('root')).render(<App />);
fetch('/api/unavailable');
