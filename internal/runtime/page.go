package runtime

const statusPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>Quota Schedule Refresh</title>
<style>
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#f8fafc;margin:0;color:#111827}
main{max-width:880px;margin:0 auto;padding:40px 20px}
section{background:#fff;border:1px solid #e5e7eb;border-radius:16px;padding:24px}
h1{margin:0 0 8px;font-size:22px}
p{color:#4b5563;line-height:1.6}
button{min-height:40px;border:0;border-radius:10px;padding:0 16px;background:#111827;color:#fff;cursor:pointer}
pre{background:#f3f4f6;padding:12px;border-radius:10px;overflow:auto}
</style>
</head>
<body>
<main>
<section>
<h1>Quota Schedule Refresh <small>v0.3.1</small></h1>
<p>每天按设定时刻通过 CPA 接口向 Codex 凭证发送一次刷新请求，用于打开新的额度窗口。</p>
<p id="statusLine">正在读取状态…</p>
<p><button id="runBtn">立即执行一次</button></p>
<pre id="output"></pre>
</section>
</main>
<script>
const STATUS="/v0/management/quota-schedule-refresh/status";
const RUN="/v0/management/quota-schedule-refresh/run";
function key(){return localStorage.getItem("cpa-management-key")||localStorage.getItem("managementKey")||""}
async function call(path, method){
  const headers={"Content-Type":"application/json"};
  const k=key();
  if(k) headers.Authorization="Bearer "+k;
  const resp=await fetch(path,{method,headers});
  const text=await resp.text();
  return text;
}
async function refresh(){
  try{
    const text=await call(STATUS,"GET");
    document.getElementById("output").textContent=text;
    const data=JSON.parse(text);
    const model=data.model||data.default_model||"-";
    document.getElementById("statusLine").textContent=
      (data.schedule_enabled?"定时已开":"定时未开")+" · "+(data.daily_at||"-")+" "+(data.timezone||"")+" · 模型 "+model+" · CPA接口 · 并发 "+(data.max_concurrency||1)+" · "+(data.last_message||"尚无执行记录");
  }catch(err){
    document.getElementById("statusLine").textContent="读取失败，请确认已登录管理端。";
    document.getElementById("output").textContent=String(err);
  }
}
document.getElementById("runBtn").onclick=async ()=>{
  document.getElementById("statusLine").textContent="正在执行…";
  try{ await call(RUN,"POST"); }catch(err){ document.getElementById("output").textContent=String(err); }
  refresh();
};
refresh();
</script>
</body>
</html>
`
