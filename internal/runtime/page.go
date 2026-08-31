package runtime

const statusPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Quota Schedule Refresh</title>
<style>
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#f8fafc;margin:0;color:#111827}
main{max-width:880px;margin:0 auto;padding:32px 20px}
section{background:#fff;border:1px solid #e5e7eb;border-radius:16px;padding:24px}
h1{margin:0 0 8px;font-size:22px;display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.badge{display:inline-flex;align-items:center;border-radius:999px;padding:3px 9px;background:#eff6ff;color:#1d4ed8;font-size:12px;font-weight:700}
p{color:#4b5563;line-height:1.6}
label{display:block;margin:16px 0 8px;font-weight:600;color:#374151}
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
.status{margin:12px 0 0;color:#374151}
.records{display:grid;gap:10px;margin-top:8px}
.card{border:1px solid #e5e7eb;border-radius:12px;padding:12px 14px;background:#f8fafc}
.card.fail{border-color:#fecaca;background:#fef2f2}
.card.ok{border-color:#bbf7d0;background:#f0fdf4}
.meta{display:flex;flex-wrap:wrap;gap:8px;align-items:center;font-size:12px;color:#6b7280;font-weight:600}
.msg{margin-top:6px;font-size:13px;word-break:break-word}
.empty{color:#9ca3af;padding:16px;text-align:center}
</style>
</head>
<body>
<main>
<section>
<div id="loginGate">
  <h1>Quota Schedule Refresh <span class="badge">v0.4.0</span></h1>
  <p>管理页嵌在 CPA Manager 侧栏中时，需要 CPA 管理密钥才能读取凭证和执行记录。</p>
  <label for="managementKey">CPA 管理密钥</label>
  <input id="managementKey" type="password" autocomplete="current-password" placeholder="请输入 CPA 管理密钥">
  <div class="actions"><button type="button" id="verifyBtn">验证密钥</button></div>
  <p class="status" id="loginMsg"></p>
</div>
<div id="appShell" hidden>
  <h1>Quota Schedule Refresh <span class="badge">v0.4.0</span></h1>
  <p>每天按设定时刻通过 CPA 接口刷新 Codex 额度窗口。也可在下方勾选凭证后手动执行。</p>
  <p class="status" id="statusLine">正在读取状态…</p>
  <label>选择要刷新的凭证</label>
  <div id="credentialList" class="list"></div>
  <div class="actions">
    <button type="button" class="secondary" id="reloadBtn">刷新列表</button>
    <button type="button" id="runBtn">刷新选中凭证</button>
  </div>
  <p class="status" id="runMsg"></p>
  <label>最近 5 条执行记录</label>
  <div id="records" class="records"></div>
</div>
</section>
</main>
<script>
const STATUS="/v0/management/quota-schedule-refresh/status";
const FILES="/v0/management/quota-schedule-refresh/auth-files";
const RUN="/v0/management/quota-schedule-refresh/run";
const KEY_NAME="cpa-management-key";
function stores(){
  const out=[];
  try{out.push(localStorage);}catch(e){}
  try{out.push(sessionStorage);}catch(e){}
  try{if(parent&&parent!==window){out.push(parent.localStorage);out.push(parent.sessionStorage);}}catch(e){}
  return out;
}
function savedKey(){
  const names=[KEY_NAME,"managementKey","cpamp-admin-key","admin-key"];
  for(const store of stores()){
    for(const name of names){
      const value=store.getItem(name);
      if(value) return value;
    }
  }
  const q=new URLSearchParams(location.search);
  return q.get("key")||q.get("management_key")||"";
}
function currentKey(){
  const typed=document.getElementById("managementKey").value.trim();
  return typed||savedKey();
}
function rememberKey(key){
  try{localStorage.setItem(KEY_NAME,key);}catch(e){}
}
async function call(path, method, body){
  const key=currentKey();
  if(!key) throw new Error("missing management key");
  const headers={"Content-Type":"application/json","Authorization":"Bearer "+key};
  const resp=await fetch(path,{method,headers,body:body?JSON.stringify(body):undefined});
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
function renderStatus(data){
  const model=data.model||data.default_model||"-";
  document.getElementById("statusLine").textContent=
    (data.schedule_enabled?"定时已开":"定时未开")+" · "+(data.daily_at||"-")+" "+(data.timezone||"")+" · 模型 "+model+" · 并发 "+(data.max_concurrency||1)+" · "+(data.last_message||"尚无执行记录");
  const records=document.getElementById("records");
  const history=data.history||[];
  if(!history.length){
    records.innerHTML='<div class="empty">暂无执行记录（重启后清空）</div>';
    return;
  }
  records.innerHTML=history.map(function(item){
    const fail=(item.results||[]).some(function(row){return !row.success;});
    const rows=(item.results||[]).map(function(row){
      const state=row.success?"成功":(row.status||"失败");
      const err=row.last_error?(" · "+row.last_error):"";
      return (row.label||row.auth_id)+" "+state+" HTTP "+(row.http_status||"-")+err;
    }).join("<br>");
    return '<div class="card '+(fail?"fail":"ok")+'"><div class="meta"><span>'+fmtTime(item.at)+'</span><span>'+(item.trigger||"-")+'</span></div><div class="msg">'+(item.message||"")+'</div><div class="msg">'+rows+'</div></div>';
  }).join("");
}
function selectedAuthIds(){
  return Array.prototype.map.call(document.querySelectorAll("#credentialList input:checked"), function(box){return box.value;});
}
async function loadFiles(){
  const data=await call(FILES,"GET");
  const files=data.files||[];
  const box=document.getElementById("credentialList");
  if(!files.length){
    box.innerHTML='<div class="empty">没有可用的 Codex 凭证</div>';
    return;
  }
  box.innerHTML=files.map(function(file){
    const label=file.label||file.auth_id;
    const extra=file.disabled?"（已禁用）":"";
    return '<label class="item'+(file.disabled?" disabled":"")+'"><input type="checkbox" value="'+file.auth_id+'" checked> '+label+extra+'</label>';
  }).join("");
}
async function loadAll(){
  const data=await call(STATUS,"GET");
  renderStatus(data);
  await loadFiles();
}
async function verify(){
  const key=currentKey();
  const msg=document.getElementById("loginMsg");
  if(!key){msg.textContent="请输入 CPA 管理密钥";return;}
  try{
    await call(STATUS,"GET");
    rememberKey(key);
    document.getElementById("loginGate").hidden=true;
    document.getElementById("appShell").hidden=false;
    await loadAll();
  }catch(err){
    msg.textContent="密钥无效或接口不可用："+err.message;
  }
}
document.getElementById("verifyBtn").onclick=verify;
document.getElementById("reloadBtn").onclick=async function(){
  try{await loadAll();}catch(err){document.getElementById("runMsg").textContent=String(err.message||err);}
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
  }catch(err){
    document.getElementById("runMsg").textContent=String(err.message||err);
  }finally{
    document.getElementById("runBtn").disabled=false;
  }
};
(async function(){
  if(savedKey()){
    document.getElementById("managementKey").value=savedKey();
    await verify();
  }
})();
</script>
</body>
</html>
`
