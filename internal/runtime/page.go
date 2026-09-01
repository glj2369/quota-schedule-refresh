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
.log-table td{padding:9px 12px;border-bottom:1px solid #f3f4f6;vertical-align:middle;white-space:nowrap}
.log-table tr:last-child td{border-bottom:0}
.log-table tr:hover td{background:#f8fafc}
.log-table tr.fail td{background:#fef2f2}
.log-table tr.fail:hover td{background:#fee2e2}
.log-table tr.batch-start td{border-top:1px solid #e5e7eb}
.log-table tr.batch-start:first-child td{border-top:0}
.log-table td.batch{color:#6b7280;font-variant-numeric:tabular-nums;font-weight:650}
.pill{display:inline-flex;align-items:center;border-radius:999px;padding:2px 8px;font-size:12px;font-weight:700;white-space:nowrap}
.pill.ok{background:#ecfdf5;color:#047857}
.pill.fail{background:#fee2e2;color:#b91c1c}
.pill.skip{background:#eff6ff;color:#1d4ed8}
.pill.muted{background:#f3f4f6;color:#4b5563}
.log-table td.reply{color:#374151}
.log-table td.reply.err{color:#b91c1c}
.clamp{max-width:320px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.mono{font-variant-numeric:tabular-nums;color:#6b7280}
.form{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:14px 18px;margin-top:12px}
.field{min-width:0}
.field label{margin:0 0 6px;font-size:13px}
.field input[type=text],.field input[type=number],.field select{width:100%;box-sizing:border-box;border:1px solid #d1d5db;border-radius:10px;padding:10px;font:inherit;background:#fff;color:inherit}
.field .hint{margin-top:6px;font-size:12px}
.toggle{display:flex;align-items:center;gap:10px;border:1px solid #d1d5db;border-radius:10px;padding:11px 12px;cursor:pointer}
.toggle:hover{background:#f9fafb}
.toggle input{width:auto;margin:0}
.toggle span{font-size:13px;font-weight:600;color:#374151}
.wide{grid-column:1/-1}
.ok-text{color:#047857}
.err-text{color:#b91c1c}
.empty{color:#9ca3af;padding:28px 16px;text-align:center}
.count{margin-left:6px;font-size:12px;font-weight:700;color:#1d4ed8}
.tab.active .count{color:#bfdbfe}
</style>
</head>
<body>
<main>
<section>
<div id="loginGate">
  <h1>Quota Schedule Refresh</h1>
  <p>正在尝试复用 CPA Manager 的登录会话。若自动读取失败，请填写 CPA 管理密钥。</p>
  <label for="managementKey">CPA 管理密钥</label>
  <input id="managementKey" type="password" autocomplete="current-password" placeholder="请输入 CPA 管理密钥">
  <div class="actions"><button type="button" id="verifyBtn">验证密钥</button></div>
  <p class="status" id="loginMsg">正在自动读取会话…</p>
</div>
<div id="appShell" hidden>
  <h1>Quota Schedule Refresh <span class="badge" id="versionBadge"></span></h1>
  <p>每天按设定时刻通过 CPA 接口刷新 Codex 额度窗口。也可手动勾选凭证执行。所有设置在「设置」页签内维护。</p>
  <p class="status" id="statusLine">正在读取状态…</p>
  <nav class="tabs">
    <button type="button" class="tab active" data-tab="run">凭证刷新</button>
    <button type="button" class="tab" data-tab="settings">设置</button>
    <button type="button" class="tab" data-tab="logs">执行记录<span class="count" id="logCount"></span></button>
  </nav>
  <div id="panel-run" class="tab-panel active">
    <label>选择要刷新的凭证</label>
    <p class="hint">只显示 Codex 账号。开启跳过 GPT Pro / Free 后，定时刷新不含这两类凭证；手动勾选仍会执行。</p>
    <div id="credentialList" class="list"></div>
    <div class="actions">
      <button type="button" class="secondary" id="reloadBtn">刷新列表</button>
      <button type="button" id="runBtn">刷新选中凭证</button>
    </div>
    <p class="status" id="runMsg"></p>
  </div>
  <div id="panel-settings" class="tab-panel">
    <label>插件设置</label>
    <p class="hint">保存后立即生效并写入插件配置文件。CPA 的 config.yaml 只作为首次启动的基线，之后以这里为准。</p>
    <div class="form">
      <div class="field">
        <label for="f_daily_at">每天触发时刻</label>
        <input id="f_daily_at" type="text" placeholder="08:00" inputmode="numeric">
        <p class="hint">格式 HH:MM，24 小时制。</p>
      </div>
      <div class="field">
        <label for="f_timezone">时区</label>
        <input id="f_timezone" type="text" placeholder="Asia/Shanghai">
        <p class="hint">IANA 时区名。</p>
      </div>
      <div class="field">
        <label for="f_model">刷新使用的模型</label>
        <input id="f_model" type="text" list="modelOptions" placeholder="留空自动选择" autocomplete="off">
        <datalist id="modelOptions"></datalist>
        <p class="hint" id="modelHint">留空则用 CPA 模型列表第一项。</p>
      </div>
      <div class="field">
        <label for="f_timeout_seconds">单次请求超时（秒）</label>
        <input id="f_timeout_seconds" type="number" min="1" max="600">
        <p class="hint">每次重试单独计时。</p>
      </div>
      <div class="field">
        <label for="f_retry_count">失败重试次数</label>
        <input id="f_retry_count" type="number" min="0" max="10">
        <p class="hint">额外重试次数，0 表示失败不重试。</p>
      </div>
      <div class="field">
        <label for="f_retry_interval_seconds">重试间隔（秒）</label>
        <input id="f_retry_interval_seconds" type="number" min="0" max="30">
        <p class="hint">两次重试之间的等待时间。</p>
      </div>
      <div class="field">
        <label for="f_prompt">刷新提示词</label>
        <input id="f_prompt" type="text" placeholder="hello">
        <p class="hint">留空沿用当前值。</p>
      </div>
      <div class="field wide">
        <label>开关</label>
        <div class="form" style="margin-top:0">
          <label class="toggle" for="f_schedule_enabled"><input id="f_schedule_enabled" type="checkbox"><span>启用定时刷新</span></label>
          <label class="toggle" for="f_skip_gpt_pro"><input id="f_skip_gpt_pro" type="checkbox"><span>定时刷新跳过 GPT Pro / Free</span></label>
          <label class="toggle" for="f_enable_disabled"><input id="f_enable_disabled" type="checkbox"><span>刷新前启用已禁用凭证</span></label>
        </div>
      </div>
    </div>
    <div class="actions">
      <button type="button" class="secondary" id="settingsReloadBtn">放弃修改</button>
      <button type="button" id="settingsSaveBtn">保存设置</button>
    </div>
    <p class="status" id="settingsMsg"></p>
    <p class="hint" id="settingsPath"></p>
  </div>
  <div id="panel-logs" class="tab-panel">
    <label>最近 5 次执行</label>
    <p class="hint">每次刷新为一批，同批凭证带序号。进程内保存，重启后清空。悬停「模型返回」可看全文。</p>
    <div id="records" class="records"></div>
  </div>
</div>
</section>
</main>
<script>
const STATUS="/v0/management/quota-schedule-refresh/status";
const FILES="/v0/management/quota-schedule-refresh/auth-files";
const RUN="/v0/management/quota-schedule-refresh/run";
const SETTINGS="/v0/management/quota-schedule-refresh/settings";
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
// 宿主把字符串里的引号等字符转成 HTML 实体后才交给我们，展示前解回来。
function unesc(value){
  return String(value==null?"":value)
    .replace(/&#(\d+);/g,function(_,dec){ return String.fromCharCode(parseInt(dec,10)); })
    .replace(/&#x([0-9a-fA-F]+);/g,function(_,hex){ return String.fromCharCode(parseInt(hex,16)); })
    .replace(/&quot;/g,"\"").replace(/&apos;/g,"'")
    .replace(/&lt;/g,"<").replace(/&gt;/g,">")
    .replace(/&nbsp;/g," ").replace(/&amp;/g,"&");
}
function triggerName(value){
  if(value==="schedule") return "定时";
  if(value==="manual") return "手动";
  return value||"-";
}
function renderStatus(data){
  skipGPTPro=!!data.skip_gpt_pro;
  // 版本只在 pluginVersion 一处维护，页面从 /status 读，避免两边写法不一致。
  document.getElementById("versionBadge").textContent=data.version?("v"+data.version):"";
  const model=data.model||data.default_model||"-";
  document.getElementById("statusLine").textContent=
    (data.schedule_enabled?"定时已开":"定时未开")+" · "+(data.daily_at||"-")+" · "+model+" · 重试 "+(data.retry_count||0)+" · "+(skipGPTPro?"跳过 Pro/Free":"含 Pro/Free");
  const records=document.getElementById("records");
  const history=data.history||[];
  const rows=[];
  history.forEach(function(item, batchIdx){
    const list=item.results||[];
    list.forEach(function(row, i){
      rows.push({at:item.at,trigger:item.trigger,row:row,batch:batchIdx+1,seq:i+1,total:list.length,first:i===0});
    });
  });
  document.getElementById("logCount").textContent=history.length?(" "+history.length):"";
  if(!rows.length){
    records.innerHTML='<div class="empty">暂无执行记录（重启后清空）</div>';
    return;
  }
  const body=rows.map(function(entry){
    const row=entry.row||{};
    const skipped=row.status==="skipped";
    const ok=!!row.success;
    const failed=!ok&&!skipped;
    const text=unesc(row.reply||row.last_error||"—");
    const detail=unesc(row.detail||row.last_error||"")||text;
    const attempts=row.attempts>1?(" · "+row.attempts+"次"):"";
    const result=skipped?"跳过":(ok?"成功":"失败");
    const pill=skipped?"skip":(ok?"ok":"fail");
    const mark="#"+entry.batch+(entry.total>1?(" · "+entry.seq+"/"+entry.total):"");
    const trClass=(failed?"fail":"")+(entry.first?" batch-start":"");
    return "<tr class=\""+trClass.trim()+"\">"+
      "<td class=\"batch\">"+esc(mark)+"</td>"+
      "<td class=\"mono\">"+(entry.first?esc(fmtTime(entry.at)):"")+"</td>"+
      "<td>"+(entry.first?("<span class=\"pill muted\">"+esc(triggerName(entry.trigger))+"</span>"):"")+"</td>"+
      "<td>"+esc(row.label||row.auth_id||"-")+"</td>"+
      "<td><span class=\"pill "+pill+"\">"+result+attempts+"</span></td>"+
      "<td class=\"mono\">"+esc(row.http_status||"-")+"</td>"+
      "<td class=\"reply"+(failed?" err":"")+"\" title=\""+esc(detail)+"\"><div class=\"clamp\">"+esc(text)+"</div></td>"+
      "</tr>";
  }).join("");
  records.innerHTML="<table class=\"log-table\"><thead><tr><th>批次</th><th>时间</th><th>类型</th><th>凭证</th><th>结果</th><th>HTTP</th><th>模型返回</th></tr></thead><tbody>"+body+"</tbody></table>";
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
    const skip=skipGPTPro&&!!file.skip_schedule;
    let badge="";
    if(file.gpt_pro){
      badge=skip?"（Pro·定时跳过）":"（Pro）";
    }else if(file.plan){
      badge=skip?"（"+file.plan+"·定时跳过）":"（"+file.plan+"）";
    }
    const extra=(file.disabled?"（已禁用）":"")+badge;
    return '<label class="item'+(file.disabled||skip?" disabled":"")+'"><input type="checkbox" value="'+esc(file.auth_id)+'"'+(skip?"":" checked")+'> '+esc(label)+extra+'</label>';
  }).join("");
}
function numValue(id, fallback){
  const raw=document.getElementById(id).value.trim();
  if(raw==="") return fallback;
  const parsed=Number(raw);
  return Number.isFinite(parsed)?parsed:fallback;
}
function fillSettings(view){
  const s=view.settings||{};
  document.getElementById("f_daily_at").value=s.daily_at||"";
  document.getElementById("f_timezone").value=s.timezone||"";
  document.getElementById("f_timeout_seconds").value=s.timeout_seconds||60;
  document.getElementById("f_retry_count").value=s.retry_count==null?2:s.retry_count;
  document.getElementById("f_retry_interval_seconds").value=s.retry_interval_seconds==null?2:s.retry_interval_seconds;
  document.getElementById("f_prompt").value=s.prompt||"";
  document.getElementById("f_schedule_enabled").checked=!!s.schedule_enabled;
  document.getElementById("f_skip_gpt_pro").checked=!!s.skip_gpt_pro;
  document.getElementById("f_enable_disabled").checked=!!s.enable_disabled;
  const models=(view.models||[]).slice();
  document.getElementById("f_model").value=s.model||"";
  document.getElementById("modelOptions").innerHTML=models.map(function(name){
    return '<option value="'+esc(name)+'"></option>';
  }).join("");
  document.getElementById("modelHint").textContent=models.length
    ?("留空则用列表第一项（"+esc(models[0])+"）。可从 "+models.length+" 个模型中选择或直接输入。")
    :"未能读取 CPA 模型列表，可直接输入模型名，或留空由凭证自身的模型列表决定。";
  document.getElementById("settingsPath").textContent=
    (view.stored?"设置文件：":"尚未保存过，将写入：")+(view.path||"-");
}
function readSettings(){
  return {
    schedule_enabled:document.getElementById("f_schedule_enabled").checked,
    daily_at:document.getElementById("f_daily_at").value.trim(),
    timezone:document.getElementById("f_timezone").value.trim(),
    model:document.getElementById("f_model").value.trim(),
    timeout_seconds:numValue("f_timeout_seconds",0),
    enable_disabled:document.getElementById("f_enable_disabled").checked,
    skip_gpt_pro:document.getElementById("f_skip_gpt_pro").checked,
    retry_count:numValue("f_retry_count",0),
    retry_interval_seconds:numValue("f_retry_interval_seconds",0),
    prompt:document.getElementById("f_prompt").value.trim()
  };
}
function settingsMessage(text, cls){
  const node=document.getElementById("settingsMsg");
  node.textContent=text;
  node.className="status"+(cls?" "+cls:"");
}
async function loadSettings(){
  fillSettings(await call(SETTINGS,"GET"));
}
async function loadAll(){
  const data=await call(STATUS,"GET");
  renderStatus(data);
  await loadSettings();
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
document.getElementById("settingsReloadBtn").onclick=async function(){
  try{
    await loadSettings();
    settingsMessage("已重新读取当前生效的设置","");
  }catch(err){
    settingsMessage(String(err.message||err),"err-text");
  }
};
document.getElementById("settingsSaveBtn").onclick=async function(){
  const btn=document.getElementById("settingsSaveBtn");
  btn.disabled=true;
  settingsMessage("正在保存…","");
  try{
    fillSettings(await call(SETTINGS,"PUT",{settings:readSettings()}));
    renderStatus(await call(STATUS,"GET"));
    await loadFiles();
    settingsMessage("已保存并生效","ok-text");
  }catch(err){
    settingsMessage(String(err.message||err),"err-text");
  }finally{
    btn.disabled=false;
  }
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
