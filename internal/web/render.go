package web

// pagesTpl holds all templates. The UI is intentionally plain HTML with
// inline SVG: no JavaScript, no external assets.
const pagesTpl = `
{{define "head"}}<!doctype html>
<html lang="zh"><head><meta charset="utf-8">
<title>时差年代账</title>
<style>
body{font-family:system-ui,-apple-system,"PingFang SC",sans-serif;margin:2rem;color:#222;max-width:1100px}
h1{font-size:1.5rem} h2{font-size:1.15rem;margin-top:2rem}
table{border-collapse:collapse;width:100%;font-size:.9rem}
th,td{border:1px solid #ddd;padding:.3rem .5rem;text-align:left;vertical-align:top}
th{background:#f0f2f5}
.ok{color:#1a7f37}.bad{color:#c00}.mut{color:#777}
code{background:#f4f4f4;padding:0 .25rem}
details summary{cursor:pointer;color:#06c}
nav a{margin-right:1rem}
form.inline{display:inline}
input,select{padding:.2rem}
</style></head><body>
<nav><a href="/">总览</a><a href="/compare">策略对比</a></nav>
{{end}}

{{define "anchorbar"}}
<form method="get" action="">
  可信锚点:
  <select name="anchor">
    <option value="">（无锚点，保留候选）</option>
    {{range .Anchors}}<option value="{{.ID}}" {{if eq $.AnchorID (printf "%d" .ID)}}selected{{end}}>{{.Name}} (unix {{.UnixSec}})</option>{{end}}
  </select>
  闰秒策略:
  <select name="leap">
    <option value="strict" {{if eq .Leap "strict"}}selected{{end}}>strict（剔除闰秒预告样本）</option>
    <option value="permissive" {{if eq .Leap "permissive"}}selected{{end}}>permissive（保留并标注）</option>
    <option value="ignore" {{if eq .Leap "ignore"}}selected{{end}}>ignore（忽略 LI）</option>
  </select>
  {{if .PeerID}}<input type="hidden" name="id" value="{{.PeerID}}">{{end}}
  <button type="submit">应用</button>
</form>
{{end}}

{{define "dash"}}
{{template "head" .}}
<h1>时差年代账</h1>
<p class="mut">离线 NTP 年代账：四时间戳、原始 64 位字段与采集时钟来源全量保留；era 不靠本机时间猜测。</p>
{{template "anchorbar" .}}
<h2>Peer 状态</h2>
<table>
<tr><th>peer</th><th>地址</th><th>reach</th><th>stratum</th><th>refid</th><th>精度指数</th><th>滤波样本</th><th>era 状态</th></tr>
{{range .Peers}}
<tr>
  <td><a href="/peer?id={{.ID}}&anchor={{$.AnchorID}}&leap={{$.Leap}}">{{.Name}}</a></td>
  <td>{{.Addr}}</td>
  <td><code>{{.ReachOctal}}</code></td>
  <td>{{.Stratum}}</td>
  <td><code>{{.RefID}}</code></td>
  <td>{{.Precision}}</td>
  <td>{{len .Samples}}</td>
  <td>{{.EraNote}}</td>
</tr>
{{end}}
</table>
<h2>锚点管理</h2>
<table>
<tr><th>名称</th><th>unix 秒</th><th>说明</th></tr>
{{range .Anchors}}<tr><td>{{.Name}}</td><td>{{.UnixSec}}</td><td>{{.Note}}</td></tr>{{end}}
</table>
<form method="post" action="/anchors">
  <input name="name" placeholder="名称" required>
  <input name="unix_sec" placeholder="unix 秒" required>
  <input name="note" placeholder="说明">
  <button type="submit">添加锚点</button>
</form>
</body></html>
{{end}}

{{define "peer"}}
{{template "head" .}}
<h1>时差年代账 · {{.Peer.Name}}</h1>
<p class="mut">{{.Peer.Addr}} · reach <code>{{.Peer.ReachOctal}}</code> · stratum {{.Peer.Stratum}} · refid <code>{{.Peer.RefID}}</code> · {{.Peer.EraNote}}</p>
{{template "anchorbar" .}}
<h2>offset / delay</h2>
{{.Chart}}
<h2>交换明细（点击展开筛选证据与原始字段）</h2>
<table>
<tr><th>seq</th><th>采集时钟</th><th>delay</th><th>offset</th><th>dispersion</th><th>root distance</th><th>era 候选</th><th>结论</th></tr>
{{range .Peer.Evals}}
<tr>
  <td>{{.Ex.Seq}}</td>
  <td>{{.Ex.CaptureClock}}</td>
  <td>{{fixedms .Delay}} ms</td>
  <td>{{fixedms .Offset}} ms</td>
  <td>{{fixedms .Dispersion}} ms</td>
  <td>{{fixedms .RootDist}} ms</td>
  <td>{{range .EraCands}}c{{.ClientEra}}/s{{.ServerEra}} {{end}}{{if .Selected}}<b>→ c{{.Selected.ClientEra}}/s{{.Selected.ServerEra}}</b>{{else}}<span class="mut">（未选定）</span>{{end}}</td>
  <td>{{if .Accepted}}<span class="ok">接受</span>{{else}}<span class="bad">拒绝: {{.Reason}}</span>{{end}}
    <details><summary>证据</summary>
      <table>
      <tr><th>过滤器</th><th>判定</th><th>说明</th></tr>
      {{range .Verdicts}}<tr><td>{{.Name}}</td><td>{{if .Reject}}<span class="bad">拒绝</span>{{else}}<span class="ok">通过</span>{{end}}</td><td>{{.Detail}}</td></tr>{{end}}
      </table>
      <p class="mut">LI={{.Ex.Resp.LI}} stratum={{.Ex.Resp.Stratum}} precision={{.Ex.Resp.Precision}}
        refid=<code>{{.Ex.Resp.RefIDString}}</code></p>
      <p class="mut">T1 {{hex32 .Ex.T1.Sec}}.{{hex32 .Ex.T1.Frac}} · T2 {{hex32 .Ex.Resp.Rx.Sec}}.{{hex32 .Ex.Resp.Rx.Frac}} ·
        T3 {{hex32 .Ex.Resp.Tx.Sec}}.{{hex32 .Ex.Resp.Tx.Frac}} · T4 {{hex32 .Ex.T4.Sec}}.{{hex32 .Ex.T4.Frac}}</p>
      <p class="mut">raw: <code>{{rawhex .Ex.Raw}}</code></p>
    </details>
  </td>
</tr>
{{end}}
</table>
<h2>滤波样本（最新 8 个）</h2>
<table>
<tr><th>seq</th><th>offset</th><th>delay</th><th>root distance</th><th>闰秒预告</th></tr>
{{range .Peer.Samples}}
<tr><td>{{.Seq}}</td><td>{{fixedms .Offset}} ms</td><td>{{fixedms .Delay}} ms</td><td>{{fixedms .RootDist}} ms</td><td>{{if .LeapFlag}}是{{else}}否{{end}}</td></tr>
{{end}}
</table>
</body></html>
{{end}}

{{define "compare"}}
{{template "head" .}}
<h1>时差年代账 · 策略对比</h1>
<form method="get" action="/compare">
  锚点:
  <select name="anchor">
    <option value="">（仅无锚点）</option>
    {{range .Anchors}}<option value="{{.ID}}" {{if eq $.AnchorID (printf "%d" .ID)}}selected{{end}}>{{.Name}}</option>{{end}}
  </select>
  <button type="submit">对比</button>
</form>
{{range .Runs}}
<h2>{{.Label}} · leap={{.Leap}}</h2>
<table>
<tr><th>peer</th><th>reach</th><th>接受样本</th><th>era 状态</th><th>各交换结论</th></tr>
{{range .Peers}}
<tr>
  <td>{{.Name}}</td>
  <td><code>{{.ReachOctal}}</code></td>
  <td>{{len .Samples}}</td>
  <td>{{.EraNote}}</td>
  <td>{{range .Evals}}{{if .Accepted}}<span class="ok">✓{{.Ex.Seq}}</span>{{else}}<span class="bad" title="{{.Reason}}">✗{{.Ex.Seq}}</span>{{end}} {{end}}</td>
</tr>
{{end}}
</table>
{{end}}
</body></html>
{{end}}
`
