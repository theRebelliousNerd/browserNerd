package browser

// This fixed script reads no DOM attributes, text, values, application props,
// event.key, callback source, or arguments. All returned strings are generated
// identities or members of fixed internal vocabularies.
const inputTraceArmScript = `function(options, binding) {
  const root=this, doc=root.ownerDocument, win=doc.defaultView;
  if(!root.isConnected || win!==window) throw new Error('trace_target_unavailable');
  const emit=win[binding];
  if(typeof emit!=='function') throw new Error('trace_binding_unavailable');
  const refs=new WeakMap(), entries=new WeakMap(), pending=new Map();
  const nodes=[], events=[], listeners=[];
  const knownTags=new Set(['html','body','main','section','div','span','button','input','textarea','select','option','a','form','label','svg','path']);
  const keys=new Set(['Enter','NumpadEnter','Space','Tab','Escape','ArrowUp','ArrowDown','ArrowLeft','ArrowRight']);
  const kinds=['pointerdown','pointerup','pointercancel','mousedown','mouseup','click','focus','blur','focusin','focusout','keydown','keyup'];
  let active=true, reason='', dropped=0, droppedPhases=0, nodeLimit=false, pathLimit=false;
  let timeout, retainedTimer, sequence=0, phaseCount=0;
  const started=performance.now();
  const ref=(node)=>{
    if(node===win)return 'window'; if(node===doc)return 'document';
    if(!node || typeof node!=='object')return 'unavailable';
    if(refs.has(node))return refs.get(node);
    if(nodes.length===64){nodeLimit=true;return 'node_limit';}
    const id='n'+nodes.length, tag=String(node.localName||'');
    refs.set(node,id); nodes.push({ref:id,kind:knownTags.has(tag)?tag:'element'});return id;
  };
  ref(root);
  const snapshot=()=>({active,reason,elapsed_ms:Math.max(0,Math.round(performance.now()-started)),
    target_ref:'n0',nodes,events,event_count:events.length,phase_count:phaseCount,
    final_confirmed:events.filter(row=>row.final.confirmed).length,
    dropped_events:dropped,dropped_phases:droppedPhases,node_limit:nodeLimit,path_limit:pathLimit});
  const post=()=>{try{emit(JSON.stringify(snapshot()));}catch(_){halt('binding_unavailable');}};
  const removeListeners=()=>{for(const [node,kind,fn,capture] of listeners)node.removeEventListener(kind,fn,capture);listeners.length=0;clearTimeout(timeout);};
  function halt(why){if(!active)return;active=false;reason=why;removeListeners();post();}
  const finalize=(event,row)=>{const task=pending.get(event);if(!task)return;
    clearTimeout(task);pending.delete(event);row.final={confirmed:true,default_prevented:!!event.defaultPrevented,propagation:'not_observable_after_dispatch'};post();};
  const relevant=(event,kind)=>{
    if(kind==='keydown'||kind==='keyup')return keys.has(event.code)&&(event.target===root||root.contains(event.target));
    const path=event.composedPath();
    if(path.includes(root))return true;
    if(kind.startsWith('pointer')||kind.startsWith('mouse')||kind==='click'){
      const r=root.getBoundingClientRect();return event.clientX>=r.left&&event.clientX<=r.right&&event.clientY>=r.top&&event.clientY<=r.bottom;
    }return false;
  };
  const observe=(event,kind,current,capture)=>{
    if(!active||!relevant(event,kind))return;
    if(!root.isConnected){halt('target_detached');return;}
    let row=entries.get(event), fresh=false;
    if(!row){
      if(events.length>=options.max_events){dropped++;halt('event_limit');return;}
      const path=event.composedPath(); if(path.length>20)pathLimit=true;
      row={event_id:++sequence,kind,at_ms:Math.max(0,Math.round(performance.now()-started)),
        target_ref:ref(event.target),path:path.slice(0,20).map(ref),within_target:path.includes(root),trusted:!!event.isTrusted,
        modifiers:{ctrl:!!event.ctrlKey,shift:!!event.shiftKey,alt:!!event.altKey,meta:!!event.metaKey},
        phases:[],final:{confirmed:false,default_prevented:null,propagation:'not_observable_after_dispatch'}};
      if(kind==='keydown'||kind==='keyup')row.key_code=event.code;
      if(kind.startsWith('pointer')||kind.startsWith('mouse')||kind==='click'){
        row.button=event.button;row.buttons=event.buttons;row.x=event.clientX;row.y=event.clientY;
      }
      events.push(row);entries.set(event,row);fresh=true;
      const task=setTimeout(()=>finalize(event,row),0);pending.set(event,task);
    }
    if(row.phases.length>=40){droppedPhases++;halt('phase_limit');return;}
    row.phases.push({current_target:current,phase:event.eventPhase,listener_capture:capture,
      default_prevented:!!event.defaultPrevented,cancel_bubble:!!event.cancelBubble});phaseCount++;
    if(JSON.stringify(snapshot()).length>options.max_trace_bytes-512){
      row.phases.pop();phaseCount--;droppedPhases++;
      if(fresh){events.pop();entries.delete(event);clearTimeout(pending.get(event));pending.delete(event);dropped++;}
      halt('byte_limit');return;
    }
    post();
  };
  const ancestry=[]; let cursor=root;
  while(cursor&&ancestry.length<16){ancestry.push(cursor);cursor=cursor.parentNode||(cursor.host||null);}
  if(cursor)pathLimit=true;
  if(!ancestry.includes(doc))ancestry.push(doc);ancestry.push(win);
  for(const node of ancestry){const current=ref(node);
    for(const kind of kinds)for(const capture of [true,false]){
      const fn=event=>observe(event,kind,current,capture);
      node.addEventListener(kind,fn,{capture,passive:true});listeners.push([node,kind,fn,capture]);
    }
  }
  const pagehide=()=>halt('navigation');
  win.addEventListener('pagehide',pagehide,{capture:true,passive:true});listeners.push([win,'pagehide',pagehide,true]);
  timeout=setTimeout(()=>halt('timeout'),options.duration_ms);
  retainedTimer=setTimeout(()=>{halt('timeout');for(const task of pending.values())clearTimeout(task);pending.clear();events.length=0;nodes.length=0;},options.duration_ms+60000);
  post();
  return {stop(why){halt(why);for(const [event] of pending)finalize(event,entries.get(event));
    clearTimeout(retainedTimer);for(const task of pending.values())clearTimeout(task);pending.clear();
    return snapshot();}};
}`
