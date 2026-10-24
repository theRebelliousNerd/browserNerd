import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import * as Tabs from '@radix-ui/react-tabs';
import { ReactFlow } from '@xyflow/react';
import '@xyflow/react/dist/style.css';

// This fixture uses real library handlers. The document ledger observes native
// input without replacing, invoking, or synthesizing those handlers.
window.inputProof = { events: [], tab: 'alpha', name: '', clicks: [], drags: 0 };
const eventRows = new WeakMap();
for (const type of ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click',
  'pointermove', 'mousemove', 'keydown', 'keyup', 'beforeinput', 'input', 'focusin']) {
  // Window capture also sees mouseup consumed by a drag library's own window
  // listener before it can reach document.
  window.addEventListener(type, (event) => {
    const target = event.target.closest?.('[data-input-target]');
    const row = {
      type, target: event.target.id || event.target.tagName,
      control: target?.dataset.inputTarget || '', key: event.key || '',
      value: event.target.value,
      trusted: event.isTrusted, button: event.button, buttons: event.buttons,
      ctrl: !!event.ctrlKey, shift: !!event.shiftKey,
      alt: !!event.altKey, meta: !!event.metaKey,
      x: event.clientX, y: event.clientY, detail: event.detail,
    };
    window.inputProof.events.push(row);
    eventRows.set(event, row);
  }, true);
  window.addEventListener(type, (event) => {
    const row = eventRows.get(event);
    if (row) {
      row.prevented = event.defaultPrevented;
      row.tabAtBubble = window.inputProof.tab;
    }
  });
}

function Card({ data }) {
  return <div id={data.id} data-testid={data.testid} data-input-target={data.target}
    role="button" aria-label={data.label}
    style={{ padding: 16, background: '#e0edff', border: '1px solid #294b7a' }}>
    <span>{data.label}</span>
  </div>;
}
const nodeTypes = { card: Card };
const nodes = ['role', 'ref', 'testid'].map((mode, index) => ({
  id: mode, type: 'card', position: { x: 20 + index * 210, y: 45 },
  data: { id: `card-${mode}`, testid: mode === 'testid' ? 'card-test' : undefined,
    target: `card-${mode}`, label: `Movable ${mode}` },
}));

function App() {
  const [tab, setTab] = useState('alpha');
  const [name, setName] = useState('');
  const [clicks, setClicks] = useState([]);
  return <main>
    <h1>Native input contract</h1>
    <label>Draft name <input data-input-target="name" value={name}
      onChange={(event) => { setName(event.target.value); window.inputProof.name = event.target.value; }} /></label>
    <Tabs.Root value={tab} activationMode="manual"
      onValueChange={(value) => { setTab(value); window.inputProof.tab = value; }}>
      <Tabs.List aria-label="Sections">
        <Tabs.Trigger value="alpha" id="tab-alpha" data-input-target="tab-alpha">Alpha</Tabs.Trigger>
        <Tabs.Trigger value="role" id="tab-role" data-input-target="tab-role"><span>Role section</span></Tabs.Trigger>
        <Tabs.Trigger value="ref" id="tab-ref" data-input-target="tab-ref"><span>Reference section</span></Tabs.Trigger>
        <Tabs.Trigger value="testid" id="tab-testid" data-testid="section-test" data-input-target="tab-testid"><span>Test section</span></Tabs.Trigger>
      </Tabs.List>
      {['alpha', 'role', 'ref', 'testid'].map((value) => <Tabs.Content key={value} value={value}>Selected {value}</Tabs.Content>)}
    </Tabs.Root>
    <div style={{ width: 740, height: 240 }}>
      <ReactFlow nodes={nodes} nodeTypes={nodeTypes} edges={[]}
        onNodeClick={(_, node) => { window.inputProof.clicks.push(node.id); setClicks([...window.inputProof.clicks]); }}
        onNodeDragStart={() => { window.inputProof.drags++; }}
        proOptions={{ hideAttribution: true }} />
    </div>
    <output aria-label="Proof state">{JSON.stringify({ tab, name, clicks })}</output>
  </main>;
}
createRoot(document.getElementById('root')).render(<App />);
