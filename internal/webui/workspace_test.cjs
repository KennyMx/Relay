// Exercise the shipped workspace code without a browser dependency.
const {test} = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const vm = require('node:vm');

function harness(fetcher) {
  const elements = new Map();
  const el = id => {
    if (!elements.has(id)) elements.set(id, {value:'',textContent:'',innerHTML:'',hidden:false,events:{},classList:{toggle(){}},setAttribute(){},replaceChildren(){this.innerHTML='';this.textContent='';},addEventListener(name, fn){this.events[name]=fn;}});
    return elements.get(id);
  };
  vm.runInNewContext(readFileSync(__dirname+'/ui/workspace.js','utf8'), {
    document:{getElementById:el,querySelectorAll:()=>[]},location:{search:''},URLSearchParams,AbortController,
    fetch:(path,options)=>path==='/health'?Promise.resolve({ok:true}):fetcher(path,options),
  });
  return {el,run:()=>el('trialForm').events.submit({preventDefault(){}}),clear:()=>el('clearTrial').events.click()};
}
const result = () => ({id:'request-123456',status:'error',routing:{route:'fast'},attempts:[{number:1,model:'<img src=x>',provider:'mock',status:'error',error_code:'upstream_error',latency_ms:0}],usage:{total_tokens:0},cost_nano_usd:0,latency_ms:1,fallback_count:0});

test('structured 502 responses remain inspectable and provider strings are escaped', async()=>{
  let request;
  const h=harness(async(path,options)=>{request={path,options};return {ok:false,status:502,json:async()=>result()};});
  h.el('trialMessage').value='hello';
  await h.run();
  assert.equal(request.path,'/v1/try');
  assert.equal(request.options.credentials,'omit');
  assert.equal(request.options.redirect,'error');
  assert.equal(h.el('resultStatus').textContent,'Error');
  assert.match(h.el('trialAttempts').innerHTML,/&lt;img src=x&gt;/);
  assert.equal(h.el('trialError').hidden,true);
  assert.equal(h.el('runTrial').disabled,false);
});

test('clearing a session aborts work and ignores a late response', async()=>{
  let release, signal;
  const h=harness((path,options)=>{signal=options.signal;return new Promise(resolve=>{release=resolve;});});
  h.el('trialMessage').value='private text';
  const pending=h.run();h.clear();
  assert.equal(signal.aborted,true);
  release({json:async()=>result()});await pending;
  assert.equal(h.el('trialMessage').value,'');
  assert.equal(h.el('trialHistory').innerHTML,'');
  assert.equal(h.el('trialResult').hidden,true);
  assert.equal(h.el('runTrial').disabled,false);
});

test('history is bounded and malformed responses show a recoverable error',async()=>{
  let malformed=false;
  const h=harness(async()=>({json:async()=>malformed?{error:{code:'workspace_busy'}}:result()}));
  for(let i=0;i<23;i++)await h.run();
  assert.equal((h.el('trialHistory').innerHTML.match(/<tr>/g)||[]).length,20);
  malformed=true;await h.run();
  assert.equal(h.el('trialError').textContent,'Workspace busy');
  assert.equal(h.el('trialError').hidden,false);
  assert.equal(h.el('runTrial').disabled,false);
});
