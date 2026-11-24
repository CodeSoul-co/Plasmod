// Dependency-free checks of page generation and local demo workflows.
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { randomUUID } = require('node:crypto');
const elements = new Map();
const events = {};
function element(selector) {
  if (!elements.has(selector)) elements.set(selector, {
    innerHTML: '', textContent: '', value: '', dataset: {},
    classList: {add(){},remove(){},toggle(){}},
    addEventListener(name,fn){this[name]=fn;},
    focus(){}, querySelector(s){return element(s);},
    querySelectorAll(){return [];},
  });
  return elements.get(selector);
}
class DemoFormData {
  constructor(form){this.data=form.data||{};}
  *[Symbol.iterator](){for(const [k,v] of Object.entries(this.data))if(!Array.isArray(v))yield [k,v];}
  getAll(k){const v=this.data[k];return Array.isArray(v)?v:v?[v]:[];}
}
const context = vm.createContext({
  console, crypto:{randomUUID}, Date, JSON, FormData:DemoFormData,
  location:{hash:'#overview'}, localStorage:{getItem(){return null;},setItem(){}},
  setTimeout(){},clearTimeout(){},
  document:{querySelector:element,activeElement:element('active'),addEventListener(n,f){(events[n]??=[]).push(f);}},
  window:{addEventListener(){},scrollTo(){}},
});
vm.runInContext(fs.readFileSync('app.js','utf8'),context);
const run = js => vm.runInContext(js,context);
const check = (expr,message) => assert.ok(run(expr),message);
function submit(id,data,dataset={}) {
  const form={id,data,dataset,querySelector:element};
  events.submit[0]({target:form,preventDefault(){}});
}
const titles = {overview:'Database Overview',agents:'Agents',sessions:'Sessions',memory:'Memories',query:'Query & Trace',objects:'Objects',sharing:'Sharing & Resolution',maintenance:'Memory Maintenance',cleanup:'Source Cleanup'};
for(const [route,title] of Object.entries(titles)) {
  context.location.hash='#'+route;
  run('render()');
  assert.ok(element('#main').innerHTML.includes('<h1>'+title+'</h1>'),route+' renders its English title');
  assert.doesNotMatch(element('#main').innerHTML,/\b(?:demo(?:nstration)?|mock|simulat\w*)\b/i);
}
for(const [key,values] of Object.entries({overview:['system'],agents:['global'],query:['recall','trace'],objects:['states','artifacts','edges','policies'],sharing:['share','conflict'],maintenance:['receipt','extraction']})) {
  for(const value of values){context.location.hash='#'+key;run(`tabs[${JSON.stringify(key)}]=${JSON.stringify(value)};render()`);assert.ok(element('#main').innerHTML.length>500,key+'/'+value);}
}
run("scope='sandbox';location.hash='#memory';render()");
assert.ok(!element('#main').innerHTML.includes('mem_8f3a21b0'),'workspace separation');
run("scope='default';tabs.query='query';location.hash='#query';render()");
submit('query-form',{query_text:'terminology',query_scope:'default',requester_agent_id:'agent_assistant',top_k:'3',response_mode:'structured_evidence'});
check("queryResponse.memories.length===1 && queryResponse.memories[0].agent_id==='agent_assistant'",'query matches text and requester');
check('db.queryTotal===25','query counter updates');
submit('query-form',{query_text:'test',query_scope:'default',requester_agent_id:'agent_research',top_k:'3',response_mode:'objects_only',target_object_ids:'mem_8f3a21b0'});
check('queryResponse.memories.length===0','private memory is not returned to another requester');
run("newMemoryForm();");
const modal=element('#modal-form');
function modalSubmit(data){modal.submit({preventDefault(){},currentTarget:{data,querySelector:element}});}
modalSubmit({agent_id:'agent_assistant',session_id:'session_a08f21',event_type:'observation',event_subtype:'conversation',event_time:String(Date.now()),importance:'1',visibility:'private',consistency:'strict',event_id:'evt_check',payload:'{"text":"验证写入内容"}'});
check("db.memories[0].content==='验证写入内容' && db.memories[0].source_event_ids[0]==='evt_check'",'event ingest creates memory and source');
run("globalThis.createdId=db.memories[0].memory_id;newMemoryForm(createdId)");
modalSubmit({agent_id:'agent_assistant',session_id:'session_a08f21',event_type:'observation',event_subtype:'conversation',event_time:String(Date.now()),importance:'1',visibility:'private',consistency:'strict',payload:'{"text":"更正后的内容"}'});
check("db.memories.find(m=>m.memory_id===createdId).lifecycle_state==='forgotten' && db.mutations[0].replacement_id===db.memories[0].memory_id",'correction replaces original and produces receipt');
run('showDetail(createdId)');
assert.match(element('#overlay-root').innerHTML,/410 Gone/,'forgotten details return 410 state');
run("closeOverlay();tabs.sharing='share';location.hash='#sharing';render()");
submit('share-form',{memory_id:'mem_8f3a21b0',from_agent_id:'agent_assistant',to_agent_id:'agent_research',share_contract_id:'contract_5d28e1'});
check("db.memories[0].agent_id==='agent_research' && db.memories[0].shared_from==='mem_8f3a21b0'",'contract-based sharing creates target memory');
submit('share-form',{memory_id:'mem_8f3a21b0',from_agent_id:'agent_assistant',to_agent_id:'agent_developer',share_contract_id:'contract_5d28e1'});
assert.match(element('.error-message').textContent,/403/,'contract rejects disallowed target');
run("maintenanceOperation='summarize'");
submit('maintenance-form',{memory_ids:['mem_8f3a21b0','mem_6a1d83f2'],limit:'1',agent_id:'',session_id:'',after_id:''});
check('db.operations[0].selected_count===1 && db.operations[0].has_more && db.operations[0].produced_ids.length===1','maintenance paging and summary receipt');
submit('receipt-form',{id:run('db.operations[0].operation_id')},{kind:'receipt'});
assert.match(element('#receipt-result').innerHTML,/produced_ids/,'operation receipt lookup');
run("scope='sandbox'");
submit('receipt-form',{id:run('db.operations[0].operation_id')},{kind:'receipt'});
assert.match(element('#receipt-result').innerHTML,/404/,'receipt is scope-bound');
run("scope='default';previewCleanup({operation:'purge',source_type:'memory_id',source_id:'mem_8f3a21b0',only_if_inactive:'true',include_memory_ids:'true'})");
check('cleanupPreview.eligible.length===0','purge excludes active memory');
run("previewCleanup({operation:'purge',source_type:'memory_id',source_id:'mem_3b6d05a8',only_if_inactive:'true',include_memory_ids:'true'})");
check('cleanupPreview.eligible.length===1','purge permits stale memory');
run("db.mode='prod';tabs.query='query';location.hash='#query'");
submit('query-form',{query_text:'terminology',query_scope:'default',requester_agent_id:'agent_assistant',top_k:'3',response_mode:'structured_evidence'});
assert.ok(!element('#main').innerHTML.includes('class="chain-card"'),'production hides chain traces');
assert.equal(run("esc('<script>')"),'&lt;script&gt;','untrusted text is escaped');
console.log('PASS: 9 pages, secondary tabs, workspace isolation, query, ingest, correction/410, sharing permissions, maintenance pagination/receipts, cleanup preview, prod mode and escaping.');
