package runtime

const statusPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Quota Schedule Refresh</title>
<style>
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#f8fafc;margin:0;color:#111827}
main{max-width:880px;margin:0 auto;padding:24px 20px}
section{background:#fff;border:1px solid #e5e7eb;border-radius:16px;padding:24px}
h1{margin:0 0 8px;font-size:22px;display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.badge{display:inline-flex;align-items:center;border-radius:999px;padding:3px 9px;background:#eff6ff;color:#1d4ed8;font-size:12px;font-weight:700}
p{color:#4b5563;line-height:1.6}
label{display:block;margin:16px 0 8px;font-weight:600;color:#374151}
.hint{margin:0;font-size:13px;color:#6b7280}
input[type=password]{width:100%;box-sizing:border-box;border:1px solid #d1d5db;border-radius:10px;padding:12px;font:inherit}
.list{border:1px solid #d1d5db;border-radius:10px;max-height:240px;overflow:auto;padding:8px}
.item{display:flex;align-items:center;gap:10px;padding:8px 10px;border-radius:8px;cursor:pointer}
.item:hover{background:#f3f4f6}
.item.disabled{color:#9ca3af}
.item input{width:auto;margin:0}
.actions{display:flex;flex-wrap:wrap;gap:10px;margin-top:16px}
button{min-height:40px;border:0;border-radius:10px;padding:0 16px;background:#111827;color:#fff;cursor:pointer;font-weight:600}
button.secondary{background:#e5e7eb;color:#111827}
button:disabled{opacity:.6;cursor:not-allowed}
.status{margin:12px 0 0;color:#374151;font-size:13px}
.tabs{display:flex;gap:8px;margin:16px 0 0;border-bottom:1px solid #e5e7eb;padding-bottom:8px}
.tab{min-height:36px;padding:0 14px;border-radius:8px;background:transparent;color:#4b5563;font-weight:600}
.tab:hover{background:#f3f4f6}
.tab.active{background:#111827;color:#fff}
.tab-panel{display:none;margin-top:8px}
.tab-panel.active{display:block}
.records{margin-top:8px;max-height:min(56vh,480px);overflow:auto;border:1px solid #e5e7eb;border-radius:12px}
.log-table{width:100%;border-collapse:collapse;font-size:13px}
.log-table th{position:sticky;top:0;background:#f8fafc;text-align:left;padding:10px 12px;color:#6b7280;font-weight:650;border-bottom:1px solid #e5e7eb;white-space:nowrap}
.log-table td{padding:9px 12px;border-bottom:1px solid #f3f4f6;vertical-align:middle}
.log-table tr:last-child td{border-bottom:0}
.log-table tr:hover td{background:#f8fafc}
.log-table tr.fail td{background:#fef2f2}
.log-table tr.fail:hover td{background:#fee2e2}
.pill{display:inline-flex;align-items:center;border-radius:999px;padding:2px 8px;font-size:12px;font-weight:700}
.pill.ok{background:#ecfdf5;color:#047857}
.pill.fail{background:#fee2e2;color:#b91c1c}
.pill.skip{background:#eff6ff;color:#1d4ed8}
.pill.muted{background:#f3f4f6;color:#4b5563}
.reply{max-width:260px;color:#374151;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;line-height:1.45;white-space:normal;word-break:break-word}
.mono{font-variant-numeric:tabular-nums;color:#6b7280}
.empty{color:#9ca3af;padding:28px 16px;text-align:center}
.count{margin-left:6px;font-size:12px;font-weight:700;color:#1d4ed8}
.tab.active .count{color:#bfdbfe}
</style>
</head>
<body>
<main>
<section>
<div id="loginGate">
  <h1>Quota Schedule Refresh <span class="badge">v0.6.6</span></h1>
  <p>正在尝试复用 CPA Manager 的登录会话。若自动读取失败，请填写 CPA 管理密钥。</p>
  <label for="managementKey">CPA 管理密钥</label>
  <input id="managementKey" type="password" autocomplete="current-password" placeholder="请输入 CPA 管理密钥">
  <div class="actions"><button type="button" id="verifyBtn">验证密钥</button></div>
  <p class="status" id="loginMsg">正在自动读取会话…</p>
</div>
<div id="appShell" hidden>
  <h1>Quota Schedule Refresh <span class="badge">v0.6.6</span></h1>
  <p>每天按设定时刻通过 CPA 接口刷新 Codex 额度窗口。也可手动勾选凭证执行。</p>
  <p class="status" id="statusLine">正在读取状态…</p>
  <nav class="tabs">
    <button type="button" class="tab active" data-tab="run">凭证刷新</button>
    <button type="button" class="tab" data-tab="logs">执行记录<span class="count" id="logCount"></span></button>
  </nav>
  <div id="panel-run" class="tab-panel active">
    <label>选择要刷新的凭证</label>
    <p class="hint">只显示 Codex 账号。开启跳过 GPT Pro 后，定时刷新不含 Pro 凭证；手动勾选仍会执行。</p>
    <div id="credentialList" class="list"></div>
    <div class="actions">
      <button type="button" class="secondary" id="reloadBtn">刷新列表</button>
      <button type="button" id="runBtn">刷新选中凭证</button>
    </div>
    <p class="status" id="runMsg"></p>
  </div>
  <div id="panel-logs" class="tab-panel">
    <label>最近 5 条执行记录</label>
    <p class="hint">最近 5 次执行，进程内保存，重启后清空。悬停「模型返回」可看全文。</p>
    <div id="records" class="records"></div>
  </div>
</div>
</section>
</main>
<script>
const STATUS="/v0/management/quota-schedule-refresh/status";
const FILES="/v0/management/quota-schedule-refresh/auth-files";
const RUN="/v0/management/quota-schedule-refresh/run";
const KEY_NAME="cpa-management-key";
const AUTH_STORE="cli-proxy-auth";
const ENC_PREFIX="enc::v1::";
const SECRET_SALT="cli-proxy-api-webui::secure-storage";
let activeKey="";
let skipGPTPro=false;
function stores(){
  const out=[];
  try{out.push(localStorage);}catch(e){}
  try{out.push(sessionStorage);}catch(e){}
  try{if(parent&&parent!==window){out.push(parent.localStorage);out.push(parent.sessionStorage);}}catch(e){}
  return out;
}
function xorDecode(payload){
  if(!payload) return "";
  if(payload.indexOf(ENC_PREFIX)!==0) return payload;
  try{
    const raw=atob(payload.slice(ENC_PREFIX.length));
    const key=new TextEncoder().encode(SECRET_SALT+"|"+location.host+"|"+navigator.userAgent);
    const bytes=new Uint8Array(raw.length);
    for(let i=0;i<raw.length;i++) bytes[i]=raw.charCodeAt(i)^key[i%key.length];
    return new TextDecoder().decode(bytes);
  }catch(e){return "";}
}
function keyFromObject(value, depth){
  if(!value||depth>5) return "";
  if(typeof value==="string"){
    const text=value.trim();
    return text&&text.length>=4?text:"";
  }
  if(typeof value!=="object") return "";
  const names=["managementKey","management_key","adminKey","admin_key","cpa-management-key"];
  for(let i=0;i<names.length;i++){
    const item=value[names[i]];
    if(typeof item==="string"&&item.trim()) return item.trim();
  }
  if(value.state) return keyFromObject(value.state, depth+1);
  return "";
}
function cpampSessionKey(){
  for(const store of stores()){
    const raw=store.getItem(AUTH_STORE);
    if(!raw) continue;
    try{
      const parsed=JSON.parse(xorDecode(raw)||raw);
      const key=keyFromObject(parsed,0);
      if(key) return key;
    }catch(e){}
  }
  return "";
}
function savedKey(){
  const names=[KEY_NAME,"managementKey","cpamp-admin-key","admin-key"];
  for(const store of stores()){
    for(const name of names){
      const value=store.getItem(name);
      if(value) return value;
    }
  }
  const fromCpamp=cpampSessionKey();
  if(fromCpamp) return fromCpamp;
  const q=new URLSearchParams(location.search);
  return q.get("key")||q.get("management_key")||"";
}
function currentKey(){
  const typed=document.getElementById("managementKey").value.trim();
  return typed||activeKey||savedKey();
}
function rememberKey(key){
  activeKey=key||"";
  if(!key) return;
  try{localStorage.setItem(KEY_NAME,key);}catch(e){}
}
function showLogin(message){
  document.getElementById("loginGate").hidden=false;
  document.getElementById("appShell").hidden=true;
  if(message) document.getElementById("loginMsg").textContent=message;
}
function showApp(){
  document.getElementById("loginGate").hidden=true;
  document.getElementById("appShell").hidden=false;
}
function switchTab(name){
  const tabs=document.querySelectorAll(".tab");
  for(let i=0;i<tabs.length;i++){
    const tab=tabs[i];
    const on=tab.getAttribute("data-tab")===name;
    tab.className=on?"tab active":"tab";
    document.getElementById("panel-"+tab.getAttribute("data-tab")).className=on?"tab-panel active":"tab-panel";
  }
}
async function call(path, method, body){
  const headers={"Content-Type":"application/json"};
  const key=currentKey();
  if(key){
    headers.Authorization="Bearer "+key;
    headers["X-Admin-Key"]=key;
    headers["X-Management-Key"]=key;
  }
  const resp=await fetch(path,{method:method,headers:headers,credentials:"include",body:body?JSON.stringify(body):undefined});
  const text=await resp.text();
  let data={};
  try{data=text?JSON.parse(text):{};}catch(e){throw new Error(text||resp.statusText);}
  if(!resp.ok) throw new Error((data&&data.error)||text||resp.statusText);
  return data;
}
function fmtTime(value){
  if(!value||value.indexOf("0001-01-01")===0) return "-";
  return value.replace("T"," ").replace("Z","").slice(0,19);
}
function esc(value){
  return String(value==null?"":value).replace(/[&<>"']/g,function(ch){
    return ({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"})[ch];
  });
}
function triggerName(value){
  if(value==="schedule") return "定时";
  if(value==="manual") return "手动";
  return value||"-";
}
function renderStatus(data){
  skipGPTPro=!!data.skip_gpt_pro;
  const model=data.model||data.default_model||"-";
  document.getElementById("statusLine").textContent=
    (data.schedule_enabled?"定时已开":"定时未开")+" · "+(data.daily_at||"-")+" · "+model+" · 并发 "+(data.max_concurrency||1)+" · 重试 "+(data.retry_count||0)+" · "+(skipGPTPro?"跳过 Pro":"含 Pro");
  const records=document.getElementById("records");
  const history=data.history||[];
  const rows=[];
  history.forEach(function(item){
    (item.results||[]).forEach(function(row){
      rows.push({at:item.at,trigger:item.trigger,row:row});
    });
  });
  document.getElementById("logCount").textContent=rows.length?(" "+rows.length):"";
  if(!rows.length){
    records.innerHTML='<div class="empty">暂无执行记录（重启后清空）</div>';
    return;
  }
  const body=rows.map(function(entry){
    const row=entry.row||{};
    const skipped=row.status==="skipped";
    const ok=!!row.success;
    const reply=row.reply||row.last_error||"—";
    const attempts=row.attempts>1?(" · "+row.attempts+"次"):"";
    const result=skipped?"跳过":(ok?"成功":"失败");
    const pill=skipped?"skip":(ok?"ok":"fail");
    return "<tr class=\""+(ok||skipped?"":"fail")+"\">"+
      "<td class=\"mono\">"+esc(fmtTime(entry.at))+"</td>"+
      "<td><span class=\"pill muted\">"+esc(triggerName(entry.trigger))+"</span></td>"+
      "<td>"+esc(row.label||row.auth_id||"-")+"</td>"+
      "<td><span class=\"pill "+pill+"\">"+result+attempts+"</span></td>"+
      "<td class=\"mono\">"+esc(row.http_status||"-")+"</td>"+
      "<td class=\"reply\" title=\""+esc(reply)+"\">"+esc(reply)+"</td>"+
      "</tr>";
  }).join("");
  records.innerHTML="<table class=\"log-table\"><thead><tr><th>时间</th><th>类型</th><th>凭证</th><th>结果</th><th>HTTP</th><th>模型返回</th></tr></thead><tbody>"+body+"</tbody></table>";
}
function selectedAuthIds(){
  return Array.prototype.map.call(document.querySelectorAll("#credentialList input:checked"), function(box){return box.value;});
}
async function loadFiles(){
  const data=await call(FILES,"GET");
  const files=data.files||[];
  const box=document.getElementById("credentialList");
  if(!files.length){
    box.innerHTML='<div class="empty">CPA 中没有可用的 Codex 凭证</div>';
    return;
  }
  box.innerHTML=files.map(function(file){
    const label=file.label||file.auth_id;
    const pro=!!file.gpt_pro;
    const skip=skipGPTPro&&pro;
    const extra=(file.disabled?"（已禁用）":"")+(pro?(skip?"（Pro·定时跳过）":"（Pro）"):(file.plan?"（"+file.plan+"）":""));
    return '<label class="item'+(file.disabled||skip?" disabled":"")+'"><input type="checkbox" value="'+esc(file.auth_id)+'"'+(skip?"":" checked")+'> '+esc(label)+extra+'</label>';
  }).join("");
}
async function loadAll(){
  const data=await call(STATUS,"GET");
  renderStatus(data);
  await loadFiles();
}
async function verify(){
  const msg=document.getElementById("loginMsg");
  try{
    await call(STATUS,"GET");
    rememberKey(currentKey());
    showApp();
    await loadAll();
  }catch(err){
    showLogin("未能自动读取会话，请填写 CPA / CPAMP 管理密钥。"+err.message);
  }
}
document.getElementById("verifyBtn").onclick=verify;
document.querySelectorAll(".tab").forEach(function(tab){
  tab.onclick=function(){switchTab(tab.getAttribute("data-tab"));};
});
document.getElementById("reloadBtn").onclick=async function(){
  try{await loadAll();document.getElementById("runMsg").textContent="已从 CPA 重新读取凭证";}catch(err){document.getElementById("runMsg").textContent=String(err.message||err);}
};
document.getElementById("runBtn").onclick=async function(){
  const ids=selectedAuthIds();
  if(!ids.length){document.getElementById("runMsg").textContent="请先勾选要刷新的凭证";return;}
  document.getElementById("runMsg").textContent="正在刷新…";
  document.getElementById("runBtn").disabled=true;
  try{
    const data=await call(RUN,"POST",{auth_ids:ids});
    renderStatus(data);
    document.getElementById("runMsg").textContent=data.last_message||"已完成";
    switchTab("logs");
  }catch(err){
    document.getElementById("runMsg").textContent=String(err.message||err);
  }finally{
    document.getElementById("runBtn").disabled=false;
  }
};
(async function(){
  const found=savedKey();
  if(found) document.getElementById("managementKey").value=found;
  await verify();
})();
</script>
</body>
</html>
`
