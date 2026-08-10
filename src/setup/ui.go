package setup

import (
	"fmt"
	"net/http"
)

const adminThemeCSS = `
@import url('https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&family=Space+Grotesk:wght@400;500;700&display=swap');
:root{
  --bg:#070707;
  --bg2:#0d0d0f;
  --panel:rgba(20,21,24,.76);
  --panel-strong:rgba(14,15,18,.92);
  --panel-soft:rgba(255,255,255,.05);
  --line:rgba(255,255,255,.11);
  --line-strong:rgba(255,255,255,.2);
  --text:#f7f7f4;
  --muted:#b8b5ae;
  --muted-2:#8f8a83;
  --accent:#e85a1a;
  --accent-2:#ff8d48;
  --accent-glow:rgba(232,90,26,.34);
  --danger:#ff6a50;
  --success:#8ecf8b;
  --shadow:0 24px 80px rgba(0,0,0,.46);
  --radius-xl:24px;
  --radius-lg:17px;
  --radius-md:14px;
  --radius-sm:10px;
}
*{box-sizing:border-box}
html,body{min-height:100%;overflow-x:hidden}
body{
  margin:0;
  color:var(--text);
  font-family:"Outfit","Noto Sans JP",sans-serif;
  font-size:13px;
  background:
    radial-gradient(circle at 20% 12%, rgba(255,119,51,.18), transparent 28%),
    radial-gradient(circle at 78% 18%, rgba(255,95,31,.12), transparent 24%),
    linear-gradient(180deg,#0a0a0b 0%, #090909 38%, #050505 100%);
  letter-spacing:.01em;
}
body::before{
  content:"";
  position:fixed;
  inset:0;
  z-index:0;
  background:
    radial-gradient(circle, rgba(255,255,255,.28) 0 1px, transparent 1.6px) 0 0/34px 34px,
    linear-gradient(135deg, rgba(255,255,255,.04), transparent 34%),
    linear-gradient(180deg, transparent, rgba(255,255,255,.02));
  pointer-events:none;
  opacity:.22;
  animation:particleDrift 18s linear infinite;
}
body::after{
  content:"";
  position:fixed;
  inset:0;
  z-index:0;
  background:radial-gradient(circle at 50% 20%, rgba(232,90,26,.14), transparent 32%);
  pointer-events:none;
  opacity:.55;
}
@keyframes particleDrift{
  from{background-position:0 0,0 0,0 0}
  to{background-position:34px 68px,0 0,0 0}
}
@keyframes riseIn{
  from{opacity:0;transform:translateY(10px)}
  to{opacity:1;transform:translateY(0)}
}
a{text-decoration:none;color:inherit}
button,input,select{font:inherit}
.shell{
  position:relative;
  z-index:1;
  max-width:1100px;
  margin:0 auto;
  padding:20px 14px 48px;
}
.topbar{
  display:flex;
  justify-content:space-between;
  gap:12px;
  align-items:center;
  margin-bottom:18px;
}
.skip-link{position:absolute;left:10px;top:-60px;padding:10px 14px;background:#fff;color:#111;border-radius:8px;z-index:20}
.skip-link:focus{top:10px}
.section-nav{display:flex;gap:8px;flex-wrap:wrap;margin:0 0 18px;padding:8px;border:1px solid var(--line);border-radius:14px;background:rgba(255,255,255,.03)}
.section-link{padding:8px 10px;border:1px solid transparent;border-radius:8px;color:var(--muted);font-weight:600;line-height:1.25;white-space:nowrap}
.section-link.active{color:var(--text);background:rgba(232,90,26,.2);border-color:rgba(232,90,26,.62);box-shadow:inset 0 -2px 0 var(--accent)}
.section-link:hover,.section-link:focus-visible{color:var(--text);background:rgba(232,90,26,.16)}
.production-list-item{display:grid;grid-template-columns:minmax(0,1fr) auto auto;gap:16px;align-items:center}
.production-list-state{display:flex;align-items:center;justify-content:flex-start;min-width:0}
.mapping-list,.status-list{display:grid;gap:10px;margin:0;padding:0;list-style:none}
.status-list{gap:8px}
.status-row{display:grid;grid-template-columns:minmax(10rem,.8fr) minmax(8rem,auto) minmax(16rem,1.5fr) minmax(10rem,auto);align-items:center;gap:12px;padding:10px 0;border-bottom:1px solid rgba(255,255,255,.08)}
.status-row:last-child{border-bottom:0}
.status-row-label{font-weight:650;color:var(--text)}
.status-row-value{display:flex;align-items:center;gap:10px;min-width:0;margin:0}
.status-row-explanation{color:var(--muted);line-height:1.45;min-width:0;overflow-wrap:anywhere}
.status-row-action{justify-self:end}
.status-row-action .btn,.status-row-action .btn-ghost{white-space:nowrap}
.pipeline-health-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin-top:14px}.system-observability-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.system-observability-grid article{min-width:0;padding:12px;border:1px solid rgba(255,255,255,.08);border-radius:12px;background:rgba(255,255,255,.025)}.system-observability-grid h3{margin:0 0 5px;font-size:15px}.system-observability-grid p{margin:0;color:var(--muted);font-size:.86rem}.pipeline-health-item{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px 14px;padding:14px;border:1px solid rgba(255,255,255,.08);border-radius:14px;background:rgba(255,255,255,.025);min-width:0}.pipeline-health-heading{display:flex;align-items:center;justify-content:space-between;gap:10px;grid-column:1 / -1}.pipeline-health-heading h3{margin:0;font-size:15px}.pipeline-health-item .field-help{margin:0;min-width:0}.pipeline-health-action{justify-self:end;align-self:end;white-space:nowrap}.pipeline-issues{display:grid;gap:8px;margin:0;padding:0;list-style:none}.pipeline-issues li{display:grid;grid-template-columns:auto minmax(0,1fr);gap:10px;padding:10px 0;border-bottom:1px solid var(--line)}.pipeline-issues li:last-child{border-bottom:0}.pipeline-issues strong{color:#ffb4a7}
.status-badge{display:inline-flex;align-items:center;gap:5px;white-space:nowrap;border-radius:999px;padding:5px 9px;border:1px solid rgba(255,255,255,.16);font-size:.78rem;font-weight:700;line-height:1.25}
.status-badge-success{color:#d7f4d4;background:rgba(142,207,139,.12);border-color:rgba(142,207,139,.42)}
.status-badge-warning{color:#fff1c4;background:rgba(255,200,80,.14);border-color:rgba(255,200,80,.44)}
.status-badge-danger{color:#ffd3ca;background:rgba(255,106,80,.13);border-color:rgba(255,106,80,.44)}
.status-badge-blocked{color:#ffe0b3;background:rgba(255,141,72,.14);border-color:rgba(255,141,72,.45)}
.status-badge-neutral{color:var(--muted);background:rgba(255,255,255,.06);border-color:rgba(255,255,255,.18)}
.connection-service-status{display:inline-flex;align-items:center;gap:8px;flex-wrap:wrap}.connection-service-name{font-weight:650;color:var(--text);min-width:5.5rem}
.mapping-list li{display:flex;justify-content:space-between;align-items:flex-start;gap:12px;padding:10px;border-bottom:1px solid var(--line);flex-wrap:wrap}.mapping-list li>*{min-width:0;overflow-wrap:anywhere}.mapping-list li>span{color:var(--muted);text-align:right}
.empty-state{display:grid;gap:6px;padding:18px;border:1px dashed rgba(255,255,255,.18);border-radius:14px;background:rgba(255,255,255,.025)}.empty-state-mark{color:var(--accent-2);font-size:1.2rem}.activity-list{display:grid;gap:0;margin:0;padding:0;list-style:none}.activity-row{display:grid;grid-template-columns:minmax(9rem,auto) minmax(0,1fr) auto;gap:12px;align-items:center;padding:12px 0;border-bottom:1px solid var(--line)}.activity-row:last-child{border-bottom:0}.activity-date{color:var(--muted);font-variant-numeric:tabular-nums}.activity-result{justify-self:end}.danger-action-block{padding:14px;border:1px solid rgba(255,106,80,.25);border-radius:14px;background:rgba(255,106,80,.035)}.danger-action-block h3{margin-top:0}
.form-stack{display:grid;gap:10px}.form-stack label{justify-self:start}.form-stack input,.form-stack select{width:100%;min-width:0}.form-action-row{display:flex;align-items:center;gap:10px}.form-action-row select{flex:1;min-width:0}.user-link-actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.user-link-actions form{margin:0}.user-link-actions .user-link-form{display:grid;grid-template-columns:minmax(14rem,1fr) auto;align-items:center;gap:8px;flex:1;min-width:0}.user-link-actions .user-link-form select{min-width:0;width:100%}.settings-block{display:grid;gap:8px;padding:12px 0;border-bottom:1px solid var(--line)}.settings-block:last-of-type{border-bottom:0}
.table-wrap:has(.user-link-grid-row) table{display:grid;grid-template-columns:minmax(130px,.8fr) minmax(160px,1fr) max-content minmax(340px,1.7fr);width:100%}.table-wrap:has(.user-link-grid-row) thead,.table-wrap:has(.user-link-grid-row) tbody{display:contents}.table-wrap:has(.user-link-grid-row) thead tr,.table-wrap:has(.user-link-grid-row) .user-link-grid-row{display:grid;grid-template-columns:subgrid;grid-column:1 / -1;align-items:center;column-gap:12px;min-height:58px}.table-wrap:has(.user-link-grid-row) th,.table-wrap:has(.user-link-grid-row) td{display:flex;align-items:center;min-width:0}.table-wrap:has(.user-link-grid-row) td{min-height:64px}.table-wrap:has(.user-link-grid-row) .user-link-actions{width:100%;display:flex;align-items:center;gap:8px}.table-wrap:has(.user-link-grid-row) .user-link-actions .user-link-form{grid-template-columns:minmax(180px,1fr) auto}.table-wrap:has(.user-link-grid-row) .user-link-actions button{white-space:nowrap;min-width:5.5rem}
.detail-list{display:grid;grid-template-columns:minmax(130px,auto) minmax(0,1fr);gap:8px 16px}
.detail-list dd{margin:0;overflow-wrap:anywhere}
.danger-zone{border-color:rgba(255,106,80,.4);margin-top:18px}
.danger-actions{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px;padding-top:14px}
@media(max-width:760px){.production-list-item{grid-template-columns:1fr}.production-list-state{min-width:0}.danger-actions{grid-template-columns:1fr}.section-nav{overflow-x:auto;flex-wrap:nowrap}.section-link{white-space:nowrap}.activity-row{grid-template-columns:1fr;gap:5px}.activity-result{justify-self:start}.mapping-list li>span{text-align:left}.status-row-action .btn,.status-row-action .btn-ghost{white-space:normal}.form-action-row{align-items:stretch;flex-direction:column}.form-action-row .btn{width:100%}}
@media(max-width:760px){.user-link-actions{align-items:stretch}.user-link-actions .user-link-form{width:100%;grid-template-columns:1fr auto}.user-link-actions .user-link-form select{grid-column:1 / -1}}
@media(max-width:760px){.table-wrap:has(.user-link-grid-row) table{display:block}.table-wrap:has(.user-link-grid-row) thead{display:none}.table-wrap:has(.user-link-grid-row) tbody{display:block}.table-wrap:has(.user-link-grid-row) .user-link-grid-row{display:grid;grid-template-columns:1fr;gap:8px;padding:12px 0;border-bottom:1px solid rgba(255,255,255,.08)}.table-wrap:has(.user-link-grid-row) td{display:grid;grid-template-columns:minmax(8rem,.8fr) minmax(0,1.2fr);gap:8px;min-height:0;padding:4px 0}.table-wrap:has(.user-link-grid-row) td::before{content:attr(data-label);color:var(--muted-2);font-size:12px}.table-wrap:has(.user-link-grid-row) td:last-child{display:block}.table-wrap:has(.user-link-grid-row) td:last-child::before{display:block;margin-bottom:6px}}
 .brand-block{
  display:flex;
  gap:8px;
  align-items:flex-start;
  color:inherit;
  text-decoration:none;
  min-width:0;
}
.eyebrow{
  color:var(--accent-2);
  text-transform:uppercase;
  letter-spacing:.24em;
  font-size:9px;
  font-family:"Space Grotesk","Outfit",sans-serif;
}
.brand-title{
  font-size:24px;
  font-weight:700;
  line-height:1.02;
  letter-spacing:-.03em;
  margin:6px 0 8px;
}
.brand-sub{
  color:var(--muted);
  max-width:680px;
  line-height:1.65;
  font-size:11px;
}
.brand-sub:empty{display:none}
.top-actions{
  display:flex;
  gap:7px;
  align-items:center;
  flex-wrap:wrap;
  justify-content:flex-end;
  flex:0 1 auto;
  min-width:0;
}
.top-actions nav{
  min-width:0;
}
.glass{
  background:var(--panel);
  backdrop-filter:blur(18px);
  -webkit-backdrop-filter:blur(18px);
  border:1px solid var(--line);
  box-shadow:var(--shadow), inset 0 1px 0 rgba(255,255,255,.08);
}
.nav-card{
  padding:6px;
  border-radius:16px;
  display:flex;
  flex-wrap:wrap;
  gap:4px;
}
.nav-chip,.home-link,.action-link{
	border-radius:999px;
	min-height:44px;
  padding:8px 12px;
  display:inline-flex;
  align-items:center;
  gap:7px;
  color:var(--muted);
  background:rgba(255,255,255,.03);
  border:1px solid rgba(255,255,255,.05);
  transition:all .18s ease;
}
.nav-chip:hover,.home-link:hover,.action-link:hover{
  color:var(--text);
  border-color:rgba(255,255,255,.18);
  transform:translateY(-1px);
}
.nav-chip.active{
  color:var(--text);
  background:linear-gradient(180deg, rgba(255,255,255,.14), rgba(255,255,255,.05));
  box-shadow:inset 0 1px 0 rgba(255,255,255,.12);
}
.lang-toggle{
  position:relative;
  display:inline-grid;
  grid-template-columns:1fr 1fr;
  gap:4px;
  align-items:center;
  min-width:104px;
  min-height:56px;
  padding:4px;
  border-radius:999px;
}
.lang-thumb{
  position:absolute;
  top:6px;
  bottom:6px;
  width:calc(50% - 4px);
  left:4px;
  border-radius:999px;
  background:linear-gradient(135deg, rgba(232,90,26,.94), rgba(255,141,72,.88));
  box-shadow:0 10px 24px rgba(232,90,26,.28);
  transition:left .18s ease;
}
.lang-toggle[data-lang="en"] .lang-thumb{left:calc(50%);}
.lang-option{
  position:relative;
  z-index:1;
  display:flex;
  align-items:center;
  justify-content:center;
  text-align:center;
  font-size:10px;
  line-height:1;
  padding:0 10px;
  min-height:44px;
  color:var(--muted);
  border-radius:999px;
  font-family:"Space Grotesk","Outfit",sans-serif;
}
.lang-option.active{color:#120804;font-weight:700}
.page-card{border-radius:24px;padding:20px;}
.page-heading{display:flex;justify-content:space-between;gap:12px;align-items:flex-start;margin-bottom:12px;}
.page-heading h1{margin:0;font-size:26px;line-height:1.03;letter-spacing:-.03em;}
.page-heading p{margin:8px 0 0;color:var(--muted);line-height:1.6;}
.toast{border-radius:14px;padding:10px 12px;margin-bottom:12px;background:rgba(142,207,139,.1);border:1px solid rgba(142,207,139,.22);color:#d8f0d6;}
.dashboard-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px;}
.tile{min-height:152px;padding:14px;border-radius:20px;position:relative;overflow:hidden;background:linear-gradient(180deg, rgba(255,255,255,.08), rgba(255,255,255,.03)),linear-gradient(140deg, rgba(232,90,26,.06), transparent 48%);border:1px solid rgba(255,255,255,.1);box-shadow:var(--shadow);transition:transform .18s ease, border-color .18s ease;}
.tile,.section-card,.page-card{animation:riseIn .42s ease both;}
.tile:hover{transform:translateY(-2px);border-color:rgba(255,255,255,.18);}
.tile::after{content:"";position:absolute;inset:auto -20% -36% auto;width:150px;height:150px;border-radius:50%;background:radial-gradient(circle, rgba(232,90,26,.22), transparent 70%);pointer-events:none;}
.tile-icon{width:38px;height:38px;border-radius:14px;display:grid;place-items:center;margin-bottom:10px;color:var(--text);background:linear-gradient(180deg, rgba(255,255,255,.12), rgba(255,255,255,.04));border:1px solid rgba(255,255,255,.12);font-size:17px;}
.tile-title{font-size:16px;font-weight:600;letter-spacing:-.02em}
.tile-sub{margin-top:6px;color:var(--muted);line-height:1.45;font-size:11px}
.section-stack{display:grid;gap:12px}
.section-card{border-radius:20px;padding:14px;background:linear-gradient(180deg, rgba(255,255,255,.06), rgba(255,255,255,.03)),linear-gradient(135deg, rgba(232,90,26,.05), transparent 56%);}
.section-card h3{margin:0 0 6px;font-size:16px;letter-spacing:-.02em;}
.connections-card{padding:18px;}
.connections-summary-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;align-items:stretch}.connections-summary-grid>.connections-card{height:100%;display:flex;flex-direction:column}.connections-summary-grid .connection-field-list{flex:1}
.connections-card-header{align-items:center;margin-bottom:14px;}
.connections-section{padding:0 0 14px;margin:0 0 14px;}
.connections-section h2{margin:0 0 8px;font-size:18px;}
.connections-section:last-of-type{border-bottom:1px solid var(--line);}
.connection-field-list{display:grid;gap:4px;margin:0;padding:0;}
.connection-field-row{display:grid;grid-template-columns:minmax(180px,240px) minmax(0,1fr);align-items:center;gap:12px;min-height:38px;}
.connection-field-row dt{color:var(--muted);font-size:12px;}
.connection-field-row dd{margin:0;min-width:0;display:flex;align-items:center;min-height:30px;}
.connection-form-field{display:grid;gap:4px;margin:0 0 10px;}
.connection-form-field:last-child{margin-bottom:0;}
.connection-form-field label{margin:0;}
.connection-form-field .field-help{margin:0;}
.connections-edit-stack{gap:10px;}
.connections-edit-summary{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:0 2px;}
.connections-edit-summary .hint{margin:0;}
.connections-edit-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;align-items:stretch;}
.connections-edit-grid>.connections-card{height:100%;display:flex;flex-direction:column;}
.connections-edit-grid .connection-save-form{display:flex;flex-direction:column;flex:1;}
.connections-edit-grid .connections-actions{margin-top:auto;}
.connections-actions{margin-top:0;padding-top:0;padding-bottom:0;}
.hint,.muted{color:var(--muted);line-height:1.7}
.form-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(210px,1fr));gap:9px;}
.form-span-2{grid-column:span 2}
label{display:block;margin:0 0 8px;color:#ddd8d0;font-size:12px;text-transform:uppercase;letter-spacing:.16em;font-family:"Space Grotesk","Outfit",sans-serif;}
input,select,textarea{width:100%;min-width:0;border-radius:14px;border:1px solid rgba(255,255,255,.12);background:rgba(8,8,10,.7);color:var(--text);padding:10px 12px;outline:none;transition:border-color .18s ease, box-shadow .18s ease, background .18s ease;}
input,select{min-height:44px;}
input:focus,select:focus,textarea:focus{border-color:rgba(255,141,72,.72);box-shadow:0 0 0 3px rgba(232,90,26,.16);}
button:focus-visible,a:focus-visible,input:focus-visible,select:focus-visible,textarea:focus-visible,summary:focus-visible{outline:3px solid #6dc3ff;outline-offset:3px;}
summary:focus-visible{border-radius:10px;}
input[readonly],input[disabled],select[disabled]{opacity:.72;cursor:not-allowed;}
.field-help{color:var(--muted-2);font-size:12px;margin-top:6px;line-height:1.55;}
.button-row{display:flex;gap:6px;align-items:center;flex-wrap:wrap;margin-top:12px;}
.btn,.btn-sm,.btn-ghost,.btn-danger{display:inline-flex;align-items:center;justify-content:center;border:none;border-radius:999px;min-height:44px;padding:8px 14px;cursor:pointer;font-weight:600;font-family:"Space Grotesk","Outfit",sans-serif;letter-spacing:.04em;transition:transform .18s ease, opacity .18s ease, box-shadow .18s ease;}
.btn:hover,.btn-sm:hover,.btn-ghost:hover,.btn-danger:hover{transform:translateY(-1px)}
.btn:disabled,.btn-sm:disabled,.btn-ghost:disabled,.btn-danger:disabled{cursor:not-allowed;opacity:.72;transform:none}
.btn:disabled,.btn-sm:disabled,.btn-ghost:disabled,.btn-danger:disabled{color:var(--muted);background:rgba(255,255,255,.06);border-color:rgba(255,255,255,.1);box-shadow:none}
.btn{color:#140904;background:linear-gradient(135deg, var(--accent), var(--accent-2));box-shadow:0 14px 30px rgba(232,90,26,.24);}
.btn-sm{color:#140904;background:linear-gradient(135deg, rgba(255,141,72,.94), rgba(232,90,26,.9));padding:6px 10px;}
.btn-ghost{color:var(--text);background:rgba(255,255,255,.06);border:1px solid rgba(255,255,255,.12);}
.btn-danger{color:#fff5f2;background:rgba(255,106,80,.18);border:1px solid rgba(255,106,80,.3);}
.status-pill,.tag{display:inline-flex;align-items:center;gap:4px;padding:5px 8px;border-radius:999px;background:rgba(255,255,255,.05);border:1px solid rgba(255,255,255,.08);color:var(--muted);font-size:12px;line-height:1.25;}
.status-pill{white-space:nowrap;}
.status-pill.ok{color:#d7f4d4;border-color:rgba(142,207,139,.28);background:rgba(142,207,139,.08)}
.status-pill.warn{color:#fff1c4;border-color:rgba(255,200,80,.3);background:rgba(255,200,80,.1)}
.status-pill.bad{color:#ffd3ca;border-color:rgba(255,106,80,.28);background:rgba(255,106,80,.08)}
@media (max-width:760px){.status-row{grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:6px 10px}.status-row-label{grid-column:1}.status-row-value{grid-column:2;justify-content:flex-start}.status-row-explanation{grid-column:1 / -1}.status-row-action{grid-column:1 / -1;justify-self:start}.status-badge{white-space:normal}.pipeline-health-grid,.system-observability-grid{grid-template-columns:1fr}.pipeline-health-item{grid-template-columns:1fr}.pipeline-health-action{justify-self:start}}
.connection-field-row{overflow-wrap:anywhere;}
@media (max-width:760px){.connection-field-row{grid-template-columns:1fr;gap:2px;min-height:0;padding:5px 0}.connection-field-row dd{min-height:28px}.connections-card{padding:14px}.connections-section{padding-bottom:12px;margin-bottom:12px}}
@media (max-width:760px){.connections-edit-summary{align-items:flex-start;flex-direction:column;gap:6px}.connections-edit-grid{grid-template-columns:1fr;}}
@media (max-width:760px){.connections-summary-grid{grid-template-columns:1fr}.connections-summary-grid>.connections-card{height:auto}}
.table-wrap{overflow:auto;border-radius:16px;border:1px solid rgba(255,255,255,.08);background:rgba(4,4,6,.35);}
table{width:100%;border-collapse:collapse}
th,td{padding:9px 10px;border-bottom:1px solid rgba(255,255,255,.07);text-align:left;vertical-align:top;overflow-wrap:anywhere;word-break:break-word}
th{color:var(--muted-2);font-size:12px;text-transform:uppercase;letter-spacing:.16em;font-family:"Space Grotesk","Outfit",sans-serif;font-weight:500;}
code{background:rgba(255,255,255,.06);padding:4px 8px;border-radius:10px;color:#fff7f0;}
.empty{text-align:center;padding:18px 12px;border-radius:16px;border:1px dashed rgba(255,255,255,.16);background:rgba(255,255,255,.03);}
.metric-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:9px;}
.dashboard-intro{display:flex;justify-content:space-between;align-items:flex-start;gap:16px;padding:2px 2px 0}.dashboard-intro h1{margin:4px 0 0;font-size:28px;line-height:1.1}.dashboard-summary-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px}.dashboard-queue{width:100%}.dashboard-queue .list-tight{display:grid;gap:0;margin:0;padding:0;list-style:none}.dashboard-queue-row{display:grid;grid-template-columns:minmax(0,1fr) auto auto;gap:14px;align-items:center;padding:13px 0;border-bottom:1px solid var(--line)}.dashboard-queue-row:last-child{border-bottom:0}.dashboard-queue-row>div{display:grid;gap:4px;min-width:0}.dashboard-lower-grid{display:grid;grid-template-columns:minmax(0,2fr) minmax(260px,1fr);gap:16px}.dashboard-lower-grid .section-card{min-width:0}.dashboard-side-stack{display:grid;gap:16px;align-content:start;min-width:0}.dashboard-status-list{display:grid;gap:0}.dashboard-status-row{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,auto);align-items:center;gap:10px;padding:9px 0;border-bottom:1px solid var(--line)}.dashboard-status-row:last-child{border-bottom:0}.dashboard-status-label{font-weight:650;overflow-wrap:anywhere}.activity-columns{display:grid;grid-template-columns:minmax(9rem,auto) minmax(0,1fr) minmax(0,1fr) auto;gap:12px;padding:0 0 8px;color:var(--muted-2);font-size:11px}.dashboard-activity-row{grid-template-columns:minmax(9rem,auto) minmax(0,1fr) minmax(0,1fr) auto}.dashboard-intro+.dashboard-summary-grid{margin-top:2px}
.dashboard-cta{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:18px 22px;border:1px solid rgba(232,90,26,.72);border-radius:18px;background:linear-gradient(105deg,rgba(232,90,26,.22),rgba(255,141,72,.08));box-shadow:0 10px 26px rgba(232,90,26,.1)}.dashboard-cta h2{margin:3px 0 4px;font-size:20px}.dashboard-cta-kicker{color:var(--accent-2);font-size:11px;font-weight:700;letter-spacing:.12em;text-transform:uppercase}.dashboard-cta p{margin:0}.dashboard-cta-action{flex:0 0 auto}.dashboard-menu-wrap{display:grid;gap:22px}.dashboard-menu{display:grid;gap:12px}.dashboard-menu-grid{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:10px}.dashboard-menu-card{display:flex;min-width:0;min-height:178px;flex-direction:column;gap:12px;padding:18px;border:1px solid rgba(255,255,255,.1);border-radius:18px;background:linear-gradient(160deg,rgba(255,255,255,.065),rgba(255,255,255,.025));color:var(--text);text-decoration:none;transition:border-color .18s ease,transform .18s ease,background .18s ease}.dashboard-menu-card:hover,.dashboard-menu-card:focus-visible{border-color:rgba(255,141,72,.62);background:linear-gradient(160deg,rgba(255,141,72,.13),rgba(255,255,255,.035));transform:translateY(-2px)}.dashboard-menu-icon{display:grid;place-items:center;width:42px;height:42px;border-radius:50%;background:rgba(232,90,26,.18);border:1px solid rgba(255,141,72,.3);color:var(--accent-2);font-weight:800}.dashboard-menu-copy{display:grid;gap:7px;min-width:0}.dashboard-menu-copy strong{font-size:17px;overflow-wrap:anywhere}.dashboard-menu-arrow{margin-top:auto;color:var(--accent-2);font-size:20px}.dashboard-quick{display:none}.section-stack>.dashboard-intro{order:1}.section-stack>.dashboard-summary-grid{order:2}.section-stack>.dashboard-queue{order:3}.section-stack>.dashboard-menu-wrap{order:4}.section-stack>.dashboard-lower-grid{order:5}
.metric-card{border-radius:16px;padding:12px;background:rgba(255,255,255,.04);border:1px solid rgba(255,255,255,.08);}.metric-card.semantic-good{border-color:rgba(142,207,139,.28)}.metric-card.semantic-good .metric-value{color:#a6e0a2}.metric-card.semantic-warning{border-color:rgba(255,200,80,.34)}.metric-card.semantic-warning .metric-value{color:#ffd978}.metric-card.semantic-danger{border-color:rgba(255,106,80,.34)}.metric-card.semantic-danger .metric-value{color:#ffb4a7}.metric-card.semantic-neutral .metric-value{color:var(--text)}
.metric-label{color:var(--muted-2);font-size:12px;text-transform:uppercase;letter-spacing:.14em;}
.metric-value{margin-top:6px;font-size:18px;font-weight:600;letter-spacing:-.03em;}
.metric-value-host{font-size:16px;line-height:1.4;word-break:break-all;}
.metric-value-host code{display:inline-block;font-size:.92em;line-height:1.4;}
.accordion{border-radius:20px;overflow:hidden;border:1px solid rgba(255,255,255,.09);background:rgba(255,255,255,.04);}
.accordion summary{list-style:none;cursor:pointer;display:flex;justify-content:space-between;align-items:flex-start;gap:10px;padding:12px 14px;flex-wrap:wrap;}
.accordion summary::-webkit-details-marker{display:none}
.accordion summary:hover{background:rgba(255,255,255,.04);}
.accordion-body{padding:0 14px 14px;}
.accordion-summary-main{flex:1 1 320px;min-width:0;}
.accordion-summary-main .tile-title{overflow-wrap:anywhere;word-break:break-word}
.accordion-summary-side{display:flex;align-items:center;justify-content:flex-end;gap:10px;flex-wrap:wrap;max-width:100%;}
.accordion-summary-side .tag{max-width:100%;overflow-wrap:anywhere;}
.accordion-trigger{display:inline-flex;align-items:center;gap:6px;padding:5px 8px;border-radius:999px;background:rgba(255,255,255,.04);border:1px solid rgba(255,255,255,.08);color:var(--text);font-family:"Space Grotesk","Outfit",sans-serif;font-size:9px;letter-spacing:.08em;text-transform:uppercase;}
.accordion-caret{width:26px;height:26px;border-radius:999px;display:grid;place-items:center;background:rgba(255,255,255,.05);border:1px solid rgba(255,255,255,.08);transition:transform .18s ease;}
.accordion[open] .accordion-caret{transform:rotate(180deg)}
.project-panel-head{display:flex;justify-content:space-between;align-items:flex-start;gap:14px;flex-wrap:wrap;margin-bottom:2px;}
.project-panel-meta{display:flex;gap:10px;align-items:center;flex-wrap:wrap;}
.project-panel-meta form{max-width:100%}
.project-panel-meta .btn-danger{max-width:100%}
.channel-name{overflow-wrap:anywhere;word-break:break-word;}
.channel-groups{display:grid;gap:8px}
.channel-group{border:1px solid rgba(255,255,255,.08);border-radius:16px;padding:12px;background:rgba(255,255,255,.04)}
.channel-header{display:flex;justify-content:space-between;align-items:center;gap:9px;margin-bottom:7px;flex-wrap:wrap}
.channel-header .channel-name{font-weight:600;color:var(--text)}
.channel-header form{margin:0;flex-shrink:0}
.task-list{list-style:none;margin:0;padding:0 0 0 18px;display:flex;flex-direction:column;gap:6px}
.task-list li{margin:0;color:var(--muted);font-size:14px}
.task-list li:before{content:"├─ ";margin-right:8px;opacity:.5}
.task-list li:last-child:before{content:"└─ "}
.project-channel-table td:last-child{width:1%;white-space:nowrap}
.project-channel-table .delete-form{display:flex;justify-content:flex-end}
.delete-modal{position:fixed;inset:0;display:none;align-items:center;justify-content:center;background:rgba(0,0,0,.72);padding:20px;z-index:1000;}
.delete-modal.open{display:flex}
.delete-box{width:min(100%, 480px);max-height:min(88vh,720px);overflow:auto;padding:16px;border-radius:20px;}
.delete-title{margin:0 0 10px;font-size:18px;letter-spacing:-.03em;}
.delete-text{color:var(--muted);line-height:1.7;margin-bottom:14px;}
.delete-input{margin-top:10px;}
.inline-actions{display:flex;gap:10px;flex-wrap:wrap;align-items:center;}
.hidden{display:none !important}
.sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);border:0;}
.sot-badge{display:flex;align-items:flex-start;gap:6px;padding:8px 11px;border-radius:10px;background:rgba(109,195,255,.08);border:1px solid rgba(109,195,255,.2);color:var(--text);font-size:.77rem;line-height:1.4;margin-bottom:4px}
.sot-badge .sot-icon{flex-shrink:0;font-size:1rem;margin-top:1px}
.sot-badge strong{color:#6dc3ff}
.notice-box{padding:8px 11px;border-radius:10px;background:rgba(142,207,139,.09);border:1px solid rgba(142,207,139,.25);color:var(--text);font-size:.77rem;line-height:1.45;margin-bottom:9px}
.notice-box strong{color:#8ecf8b}
.setup-steps{display:flex;align-items:center;gap:0;margin-bottom:16px;padding:12px 0 4px}
.setup-step{display:flex;align-items:center;gap:8px;font-size:.875rem}
.step-num{width:26px;height:26px;border-radius:50%;display:flex;align-items:center;justify-content:center;font-weight:700;font-size:.8rem;flex-shrink:0}
.setup-step.done .step-num{background:#8ecf8b;color:#1a2a1a}
.setup-step.done .step-label{color:#8ecf8b}
.setup-step.active .step-num{background:#6dc3ff;color:#0d1a2a}
.setup-step.active .step-label{color:var(--text);font-weight:600}
.setup-step.pending .step-num{background:rgba(255,255,255,.12);color:var(--muted)}
.setup-step.pending .step-label{color:var(--muted)}
.setup-step.blocked .step-num{background:rgba(255,141,72,.12);color:#ffd9b8;border:1px dashed rgba(255,141,72,.55)}
.setup-step.blocked .step-label{color:#d8a982}
.step-connector{flex:1;height:2px;background:rgba(255,255,255,.1);margin:0 12px;min-width:24px}
.workflow-overview{padding:16px;background:linear-gradient(180deg, rgba(255,255,255,.08), rgba(255,255,255,.03)),linear-gradient(135deg, rgba(109,195,255,.06), rgba(232,90,26,.05) 64%, transparent);}
.workflow-overview-head{margin-bottom:8px}
.workflow-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:9px;margin-top:9px}
.workflow-card{border-radius:16px;padding:12px 12px 13px;background:rgba(255,255,255,.04);border:1px solid rgba(255,255,255,.08);display:grid;gap:8px;align-content:start;min-height:158px}
.workflow-card h3{margin:0;font-size:14px;line-height:1.2}
.workflow-card p{margin:0}
.workflow-card-head{display:flex;justify-content:space-between;align-items:center;gap:10px;flex-wrap:wrap}
.workflow-step-badge{width:24px;height:24px;border-radius:8px;display:grid;place-items:center;font-weight:700;font-family:"Space Grotesk","Outfit",sans-serif;background:rgba(255,255,255,.08);border:1px solid rgba(255,255,255,.1)}
.wizard-plan-card,.wizard-review-card{display:grid;gap:14px}.wizard-plan-card .page-heading{margin-bottom:0}.wizard-plan-table table{min-width:720px}.wizard-plan-table th,.wizard-plan-table td{vertical-align:middle}.wizard-plan-table tbody tr{transition:background .15s ease}.wizard-plan-table tbody tr.is-dragging{opacity:.45;background:rgba(255,141,72,.12)}.wizard-drag-handle{display:inline-flex;align-items:center;justify-content:center;width:24px;height:24px;margin-right:8px;border:1px solid rgba(255,255,255,.12);border-radius:7px;color:var(--muted);cursor:grab}.wizard-channel-input{width:calc(100% - 18px);min-width:120px}.wizard-channel-prefix{color:var(--muted-2);margin-right:4px}.wizard-move-controls{display:inline-flex;gap:3px;margin-left:8px}.wizard-move{min-width:28px;min-height:28px;padding:2px 7px;border:1px solid rgba(255,255,255,.12);border-radius:7px;background:rgba(255,255,255,.04);color:var(--muted);cursor:pointer}.wizard-move:hover,.wizard-move:focus-visible{color:var(--text);border-color:rgba(255,141,72,.7);background:rgba(255,141,72,.12)}.wizard-plan-status{display:inline-flex;align-items:center;margin-left:7px;padding:3px 8px;border-radius:999px;font-size:.72rem;line-height:1.25;white-space:nowrap}.wizard-plan-status-create{background:rgba(142,207,139,.1);color:#9ddd99}.wizard-plan-status-reuse{background:rgba(109,195,255,.1);color:#8bd2ff}.wizard-plan-status-conflict,.wizard-plan-status-blocked{background:rgba(255,141,72,.14);color:#ffc08b}.wizard-plan-actions{justify-content:flex-end}.wizard-connection-summary{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px;margin:0}.wizard-connection-summary>div{padding:12px 14px;border:1px solid rgba(255,255,255,.08);border-radius:12px;background:rgba(255,255,255,.035)}.wizard-connection-summary dt{color:var(--muted-2);font-size:.72rem;margin-bottom:5px}.wizard-connection-summary dd{margin:0;font-weight:650;overflow-wrap:anywhere}.wizard-count-summary{margin:0;padding:12px 14px;border-left:3px solid var(--accent-2);background:rgba(232,90,26,.08);font-weight:650}.wizard-final-channel-list{display:grid;gap:6px;margin:0;padding-left:28px}.wizard-final-channel-list li{padding:7px 10px;border-radius:9px;background:rgba(255,255,255,.035);overflow-wrap:anywhere}.wizard-final-channel-list code{color:var(--text)}.wizard-confirm-control{display:flex;align-items:center;gap:8px;padding:8px 0;font-size:.86rem}.wizard-confirm-control input{width:16px;height:16px;margin:0;appearance:auto;accent-color:var(--accent-2);outline:none}.wizard-confirm-control input:focus-visible{outline:2px solid var(--accent-2);outline-offset:3px}.wizard-confirm-control:focus-within{outline:none}
.workflow-card-actions{margin-top:auto}
.workflow-card-actions .btn-ghost{width:100%;justify-content:center}
.workflow-card-done{border-color:rgba(142,207,139,.28);background:rgba(142,207,139,.07)}
.workflow-card-done .workflow-step-badge{background:rgba(142,207,139,.18);color:#d7f4d4;border-color:rgba(142,207,139,.3)}
.workflow-card-active{border-color:rgba(109,195,255,.3);background:linear-gradient(180deg, rgba(109,195,255,.12), rgba(109,195,255,.04));box-shadow:0 0 0 1px rgba(109,195,255,.12) inset}
.workflow-card-active .workflow-step-badge{background:rgba(109,195,255,.22);color:#e9f7ff;border-color:rgba(109,195,255,.32)}
.workflow-card-pending{border-color:rgba(255,255,255,.08);background:rgba(255,255,255,.03)}
.workflow-card-pending .workflow-step-badge{color:var(--muted)}
.workflow-card-emphasis{transform:translateY(-2px)}
.workflow-primary{border:1px solid rgba(109,195,255,.22);box-shadow:0 0 0 1px rgba(109,195,255,.1) inset;background:linear-gradient(180deg, rgba(109,195,255,.08), rgba(255,255,255,.03))}
.workflow-routing-form{border:1px solid rgba(255,141,72,.24);box-shadow:0 0 0 1px rgba(255,141,72,.1) inset;background:linear-gradient(180deg, rgba(255,141,72,.08), rgba(255,255,255,.03))}
.workflow-status-card{background:linear-gradient(180deg, rgba(255,255,255,.05), rgba(255,255,255,.03))}
.workflow-support-card{background:rgba(255,255,255,.025);border-style:dashed}
.channel-preview-box{padding:10px 11px;border-radius:10px;background:rgba(109,195,255,.06);border:1px solid rgba(109,195,255,.15);margin-bottom:4px}
.preview-title{font-size:.82rem;color:#6dc3ff;font-weight:600;text-transform:uppercase;letter-spacing:.08em;margin-bottom:10px}
.preview-channels{display:flex;flex-direction:column;gap:6px}
.preview-channel{display:flex;gap:10px;align-items:baseline;font-size:.88rem}
.preview-ch-name{color:var(--text);font-weight:600;min-width:180px;flex-shrink:0}
.preview-tasks{color:var(--muted);font-size:.82rem}
.setup-inventory{display:flex;flex-direction:column;gap:16px;padding:4px 0}
.inventory-group{}
.inventory-label{font-size:.8rem;font-weight:700;text-transform:uppercase;letter-spacing:.1em;margin-bottom:6px}
.inventory-label.ok{color:#8ecf8b}
.inventory-label.fail{color:#ff6a50}
.inventory-label.warn{color:#ffc850}
.inventory-label.rolled{color:#b8b5ae}
.inventory-list{list-style:none;margin:0;padding:0 0 0 16px;display:flex;flex-direction:column;gap:4px;font-size:.88rem}
@media (max-width:1100px){.dashboard-menu-grid{grid-template-columns:repeat(3,minmax(0,1fr))}}
@media (max-width:960px){.topbar{flex-wrap:wrap}.top-actions{flex:1 1 100%;justify-content:flex-end}.page-card{padding:14px}.page-heading{flex-direction:column}.dashboard-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.dashboard-summary-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.dashboard-lower-grid{grid-template-columns:1fr}.dashboard-menu-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.workflow-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media (max-width:640px){.shell{padding:12px 9px 32px}.topbar{gap:10px;align-items:flex-start}.top-actions{width:100%;gap:6px;justify-content:flex-start;align-items:stretch}.top-actions nav{flex:1 1 100%;width:100%}.brand-title{font-size:21px}.page-heading h1{font-size:22px}.dashboard-intro{flex-direction:column;gap:8px}.dashboard-cta{align-items:stretch;flex-direction:column;padding:15px}.dashboard-cta-action{width:100%}.dashboard-menu-grid{grid-template-columns:1fr}.dashboard-summary-grid{grid-template-columns:1fr}.dashboard-queue-row{grid-template-columns:1fr;gap:8px}.dashboard-queue-row .btn-ghost{justify-self:start}.page-card{width:auto!important;max-width:100%!important;margin-left:0!important;margin-right:0!important}.section-card{padding:12px;min-width:0}.dashboard-grid,.metric-grid,.form-grid,.workflow-grid{grid-template-columns:1fr}.form-span-2{grid-column:auto}.nav-card{width:100%;justify-content:flex-start}.nav-chip{flex:1 1 auto;justify-content:center}.lang-toggle{min-width:100px}.btn,.btn-sm,.btn-ghost,.btn-danger{max-width:100%;white-space:normal}.accordion{border-radius:16px}.accordion summary{padding:11px}.accordion-body{padding:0 9px 11px}.accordion-summary-main{flex-basis:100%}.accordion-summary-side{width:100%;justify-content:flex-start}.accordion-trigger{width:100%;justify-content:space-between}.project-panel-head{flex-direction:column}.project-panel-meta{width:100%;gap:8px}.project-panel-meta .tag{max-width:100%;white-space:normal}.project-panel-meta form,.project-panel-meta .btn-danger{width:100%}.table-wrap{margin:0 -2px}.project-channel-table{overflow:visible;background:transparent;border:none}.project-channel-table thead{display:none}.project-channel-table table,.project-channel-table tbody,.project-channel-table tr,.project-channel-table td{display:block;width:100%}.project-channel-table tr{margin:0 0 8px;padding:8px;border:1px solid rgba(255,255,255,.08);border-radius:14px;background:rgba(255,255,255,.035)}.project-channel-table td{padding:5px 0;border-bottom:none}.project-channel-table td::before{content:attr(data-label);display:block;margin-bottom:4px;color:var(--muted-2);font-size:10px;text-transform:uppercase;letter-spacing:.14em;font-family:"Space Grotesk","Outfit",sans-serif}.project-channel-table td:last-child{white-space:normal}.project-channel-table .delete-form{justify-content:stretch}.project-channel-table .delete-form .btn-danger{width:100%}.delete-modal{padding:9px}.delete-box{padding:12px;border-radius:15px}.metric-value-host{font-size:14px}.workflow-overview{padding:12px}.workflow-card{min-height:auto}.setup-steps{flex-wrap:wrap;row-gap:9px}}
@media (max-width:480px){.setup-steps{display:grid;grid-template-columns:1fr;gap:6px}.setup-steps .step-connector{width:2px;height:8px;min-width:2px;margin:0 0 0 12px}.setup-step{min-height:28px}}
@media (max-width:480px){.section-card{padding:9px;box-shadow:none}.glass{box-shadow:none}.shell{padding:7px 6px 28px}.hint{font-size:.76rem}.metric-label{font-size:.7rem}.metric-value{font-size:1.05rem}.section-card h3{font-size:.92rem}.table-wrap table{font-size:.75rem}.field-help,.field-label{font-size:.76rem}}
@media (max-width:960px){.primary-nav{min-width:0;max-width:100%;overflow:hidden}.primary-nav .nav-card{flex-wrap:nowrap;max-width:100%;overflow-x:auto;overflow-y:hidden;scrollbar-width:thin;overscroll-behavior-x:contain}.primary-nav .nav-chip{flex:0 0 auto}}
@media (max-width:640px){.top-actions{flex-direction:column;align-items:stretch}.top-actions nav{width:100%;flex:0 0 auto}.lang-toggle{align-self:flex-start}}
 :root{--space-1:4px;--space-2:8px;--space-3:12px;--space-4:16px;--space-5:24px;--space-6:32px;--space-action-section:24px}.dashboard-menu-card{min-height:226px;padding:20px}.dashboard-menu-status{display:flex;flex-wrap:wrap;gap:6px;margin-top:auto}.dashboard-status-chip{display:inline-flex;align-items:center;min-height:24px;padding:3px 8px;border:1px solid rgba(255,255,255,.14);border-radius:999px;background:rgba(255,255,255,.06);color:var(--muted);font-size:11px;font-weight:700;line-height:1.2}.dashboard-status-chip.ok{border-color:rgba(142,207,139,.28);background:rgba(142,207,139,.12);color:#a6e0a2}.dashboard-status-chip.warning{border-color:rgba(255,200,80,.34);background:rgba(255,200,80,.12);color:#ffd978}.dashboard-status-chip.muted{color:var(--muted-2)}.dashboard-menu-card .dashboard-menu-arrow{margin-top:0}.wizard-connection-summary{grid-template-columns:1fr}.wizard-plan-table table{min-width:650px}.wizard-plan-actions{justify-content:flex-start}
 .dashboard-menu-status{display:flex;align-items:center;flex-wrap:nowrap;gap:6px;margin-top:auto;min-width:0}.dashboard-status-chip{white-space:nowrap}.dashboard-service-status{display:flex;align-items:center;justify-content:flex-start;gap:24px;width:100%;min-width:0}.dashboard-service-status>span{display:flex;align-items:center;gap:8px;min-width:0;flex:0 0 auto}.dashboard-service-status strong{font-size:11px;color:var(--muted)}.api-observation-card{display:grid;gap:8px}.api-observation-summary{display:flex;align-items:center;justify-content:space-between;gap:8px}.api-observation-latency{display:grid;gap:2px;font-variant-numeric:tabular-nums;color:var(--muted)}.api-observation-latency strong{font-size:20px;line-height:1.1;color:var(--text)}.api-observation-label{font-size:12px;color:var(--muted)}.api-observation-meta{font-size:11px;color:var(--muted-2)}.api-observation-not-checked{padding:20px 0;color:var(--muted)}.api-sparkline{display:block;width:100%;height:104px;border-radius:8px;background:rgba(255,255,255,.025)}.api-sparkline .bar-success{fill:#8ecf8b}.api-sparkline .bar-failure{fill:#ff6a50}.api-sparkline .chart-axis{stroke:rgba(255,255,255,.22);stroke-width:1}.api-sparkline .chart-axis-label,.api-sparkline .chart-tick,.api-sparkline .chart-time-label{fill:var(--muted-2);font-size:8px}.system-live-indicator{display:inline-flex;align-items:center;gap:6px;color:var(--muted-2);font-size:11px;white-space:nowrap}.system-live-indicator i{width:7px;height:7px;border-radius:50%;background:#8ecf8b;box-shadow:0 0 0 3px rgba(142,207,139,.1)}.telemetry-window-actions{display:flex;align-items:center;gap:12px}.telemetry-window-control{display:inline-flex;align-items:center;gap:6px;color:var(--muted-2);font-size:11px;white-space:nowrap}.telemetry-window-control select{width:auto;min-width:8rem;padding:6px 28px 6px 9px;border-radius:8px;font-size:12px}.system-observability,.pipeline-health,.system-issues{margin-top:0}.system-observability-grid{gap:16px}.pipeline-health-grid{gap:16px;margin-top:16px}.pipeline-health-details{margin-top:12px;border-top:1px solid var(--line);padding-top:10px}.pipeline-health-details summary{cursor:pointer;color:var(--muted);font-size:12px}.pipeline-detail-list{display:grid;gap:8px;margin:10px 0 0}.pipeline-detail-list>div{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px}.pipeline-detail-list dt{color:var(--muted-2);font-size:12px}.pipeline-detail-list dd{margin:0;text-align:right;font-variant-numeric:tabular-nums}.system-status-sections{gap:24px}.connections-edit-grid{gap:16px}.dashboard-menu-wrap,.section-stack{gap:24px}.button-row.connections-actions,.button-row.connections-navigation{gap:12px;margin-top:var(--space-action-section,24px)}.connections-edit-grid .connections-actions{margin-top:var(--space-action-section,24px)}.status-pill.success{color:#d7f4d4;border-color:rgba(142,207,139,.28);background:rgba(142,207,139,.08)}.status-pill.warning{color:#fff1c4;border-color:rgba(255,200,80,.3);background:rgba(255,200,80,.1)}.status-pill.danger,.status-pill.blocked{color:#ffd3ca;border-color:rgba(255,106,80,.28);background:rgba(255,106,80,.08)}.status-pill.neutral{color:var(--muted);border-color:rgba(255,255,255,.12);background:rgba(255,255,255,.04)}
 @media (max-width:760px){.dashboard-service-status{gap:12px;flex-wrap:wrap}.telemetry-window-actions{align-items:flex-start;flex-direction:column;gap:8px}.telemetry-window-control{width:100%;justify-content:space-between}.telemetry-window-control select{flex:1}}
  @media (max-width:640px){.dashboard-menu-status{flex-wrap:wrap}.dashboard-service-status{flex-wrap:wrap;gap:8px}}
  /* Current IA order follows the source DOM; clear the obsolete visual reordering rules. */
  .section-stack>.dashboard-intro,.section-stack>.dashboard-summary-grid,.section-stack>.dashboard-queue,.section-stack>.dashboard-cta,.section-stack>.dashboard-menu,.section-stack>.dashboard-menu-wrap,.section-stack>.dashboard-lower-grid{order:initial}
  .dashboard-service-status{display:grid;gap:8px;width:100%;min-width:0}
  .dashboard-service-status>span{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:8px;min-width:0}
  .dashboard-service-status strong{min-width:0;color:var(--muted);font-size:11px}
  .api-observation-card{grid-template-rows:auto minmax(0,1fr);min-width:0;min-height:224px}
  .api-observation-details{display:flex;min-height:168px;flex-direction:column}
  .api-observation-details .api-observation-latency{min-height:58px}
  .api-observation-details .api-sparkline{margin-top:auto}
  .api-observation-details .api-observation-latency{min-height:64px;gap:4px}
  .api-observation-details .api-observation-latency strong{font-size:24px;line-height:1.05;letter-spacing:-.02em}
  .api-observation-details .api-observation-meta{min-height:16px}
  .api-observation-details .api-sparkline{margin-top:8px}
  .api-sparkline{height:auto;aspect-ratio:466 / 104}
  .system-status-sections h2{font-size:1.35rem}
  .system-status-sections .system-observability-grid h3,.system-status-sections .pipeline-health-heading h3{font-size:16px}
  .system-status-sections .api-observation-latency strong{font-size:22px}
  .system-status-sections .api-observation-label,.system-status-sections .pipeline-health-item .field-help{font-size:13px}
  .system-status-sections .api-observation-meta{font-size:12px}
  .system-status-sections .api-sparkline .chart-axis-label,.system-status-sections .api-sparkline .chart-tick,.system-status-sections .api-sparkline .chart-time-label{font-size:9px}
  .system-status-sections .pipeline-health-details summary{font-size:13px}
  main:has(.system-status-sections) .page-heading h1{font-size:32px}
  .system-status-sections h2{font-size:24px}
  .system-status-sections .page-heading .hint{font-size:15px}
  .system-status-sections .system-observability-grid h3,.system-status-sections .pipeline-health-heading h3{font-size:18px}
  .system-status-sections .api-observation-latency strong{font-size:26px}
  .system-status-sections .api-observation-label,.system-status-sections .pipeline-health-item .field-help{font-size:15px}
  .system-status-sections .api-observation-meta{font-size:14px}
  .system-status-sections .api-sparkline .chart-axis-label,.system-status-sections .api-sparkline .chart-tick,.system-status-sections .api-sparkline .chart-time-label{font-size:12px}
  .system-status-sections .pipeline-health-details summary{font-size:14px}
`

func shellHeadExtras() string {
	return `<script>
(function(){
  try {
    var qs = new URLSearchParams(window.location.search);
    var current = qs.get('lang');
    var stored = localStorage.getItem('admin_lang');
    if (!current && stored) {
      qs.set('lang', stored);
      window.location.replace(window.location.pathname + '?' + qs.toString());
      return;
    }
    if (current) {
      localStorage.setItem('admin_lang', current);
    }
  } catch (e) {}
})();
</script><style>.wizard-plan-table tbody tr{cursor:grab}.wizard-plan-table tbody tr:focus-visible{outline:2px solid var(--accent-2);outline-offset:-2px}.wizard-plan-table tbody tr.drag-over{box-shadow:inset 0 -3px 0 var(--accent-2)}.wizard-exclude{width:28px;height:28px;border:0;border-radius:7px;background:transparent;color:var(--muted);cursor:pointer;white-space:nowrap;word-break:keep-all}.wizard-exclude:hover,.wizard-exclude:focus-visible{color:#ffae8b;background:rgba(255,141,72,.12);outline:2px solid rgba(255,141,72,.5);outline-offset:2px}.wizard-add-task-type{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.wizard-add-task-type-options{display:flex;align-items:end;gap:8px;flex-wrap:wrap}.wizard-add-task-type-options label{width:100%;margin:0}.wizard-add-task-type-options select{min-width:220px;max-width:100%}.wizard-add-toggle[disabled]{cursor:not-allowed;opacity:.6}</style>`
}

func langToggleHTML(r *http.Request, lang string) string {
	return fmt.Sprintf(
		`<a class="lang-toggle glass" data-lang="%s" href="%s" aria-label="Toggle language"><span class="lang-thumb"></span><span class="lang-option %s">JP</span><span class="lang-option %s">EN</span></a>`,
		lang,
		toggleLangURL(r),
		map[bool]string{true: "active", false: ""}[lang == "ja"],
		map[bool]string{true: "active", false: ""}[lang == "en"],
	)
}

func baseAdminJS(lang string) string {
	requiredPrompt := t(lang, "上に表示された確認ワードを正確に入力してください。", "Enter the exact confirmation phrase shown above.")
	authorizingText := t(lang, "認証中...", "Authorizing...")
	return fmt.Sprintf(`
<script>
(function(){
  var modal = document.getElementById('deleteModal');
  var textEl = document.getElementById('deleteModalText');
  var helperEl = document.getElementById('deleteModalHelper');
  var inputWrap = document.getElementById('deleteModalInputWrap');
  var inputEl = document.getElementById('deleteModalInput');
  var expectedEl = document.getElementById('deleteModalExpected');
  var confirmBtn = document.getElementById('deleteConfirmBtn');
  var cancelBtn = document.getElementById('deleteCancelBtn');
  var activeForm = null;
  var activeTrigger = null;
  var expectedValue = "";
  var savingLabel = %q;

  function closeModal(){
    if(!modal){ return; }
    modal.classList.remove('open');
    activeForm = null;
    expectedValue = "";
    if(inputEl){ inputEl.value = ""; }
    if(inputWrap){ inputWrap.classList.add('hidden'); }
    if(helperEl){ helperEl.classList.add('hidden'); }
    if(confirmBtn){ confirmBtn.disabled = false; }
    if(activeTrigger && activeTrigger.focus){ activeTrigger.focus(); }
    activeTrigger = null;
  }

  function focusableInModal(){
    if(!modal){ return []; }
    return Array.prototype.slice.call(
      modal.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])')
    ).filter(function(node){
      return !node.disabled && node.offsetParent !== null;
    });
  }

  function syncConfirmState(){
    if(!confirmBtn){ return; }
    if(!expectedValue){
      confirmBtn.disabled = false;
      return;
    }
    confirmBtn.disabled = !inputEl || inputEl.value !== expectedValue;
  }

  document.querySelectorAll('form.delete-form').forEach(function(form){
    form.addEventListener('submit', function(event){
      if(!modal){ return; }
      event.preventDefault();
      activeForm = form;
      activeTrigger = event.submitter || form.querySelector('button[type="submit"]');
      textEl.textContent = form.getAttribute('data-confirm') || '';
      expectedValue = form.getAttribute('data-require-text') || '';
      if(expectedValue){
        inputWrap.classList.remove('hidden');
        helperEl.classList.remove('hidden');
        expectedEl.textContent = expectedValue;
        helperEl.textContent = %q;
      } else {
        inputWrap.classList.add('hidden');
        helperEl.classList.add('hidden');
      }
      inputEl.value = '';
      syncConfirmState();
      modal.classList.add('open');
      if(expectedValue){
        window.setTimeout(function(){ inputEl.focus(); }, 20);
      }
    });
  });

  document.querySelectorAll('form').forEach(function(form){
	if(form.classList.contains('delete-form') || form.classList.contains('wizard-plan-form')){ return; }
    form.addEventListener('submit', function(event){
      if(form.dataset.submitting === '1'){
        event.preventDefault();
        return;
      }
      form.dataset.submitting = '1';
      form.querySelectorAll('button[type="submit"], input[type="submit"]').forEach(function(button){
        button.disabled = true;
        if(button.tagName === 'BUTTON'){
          button.dataset.originalText = button.textContent;
          button.textContent = savingLabel;
        }
      });
    });
  });

  if(inputEl){ inputEl.addEventListener('input', syncConfirmState); }
  if(confirmBtn){
    confirmBtn.addEventListener('click', function(){
      if(activeForm && !confirmBtn.disabled){
        if(expectedValue){
          var hidden = activeForm.querySelector('input[name="confirm_text"]');
          if(!hidden){
            hidden = document.createElement('input');
            hidden.type = 'hidden';
            hidden.name = 'confirm_text';
            activeForm.appendChild(hidden);
          }
          hidden.value = inputEl.value;
        }
        confirmBtn.disabled = true;
        confirmBtn.textContent = savingLabel;
        activeForm.submit();
      }
    });
  }
  if(cancelBtn){ cancelBtn.addEventListener('click', closeModal); }
  if(modal){
    modal.addEventListener('click', function(event){
      if(event.target === modal){ closeModal(); }
    });
  }
  document.addEventListener('keydown', function(event){
    if(!modal || !modal.classList.contains('open')){ return; }
    if(event.key === 'Escape'){ closeModal(); return; }
    if(event.key !== 'Tab'){ return; }
    var items = focusableInModal();
    if(items.length === 0){ return; }
    var first = items[0];
    var last = items[items.length - 1];
    if(event.shiftKey && document.activeElement === first){
      event.preventDefault();
      last.focus();
      return;
    }
    if(!event.shiftKey && document.activeElement === last){
      event.preventDefault();
      first.focus();
    }
  });

  document.querySelectorAll('[data-edit-lock-link]').forEach(function(link){
    link.addEventListener('click', function(){
      link.dataset.originalText = link.textContent;
      link.textContent = %q;
    });
  });
})();

// Toggle custom channel name input in the "Add channel" form.
// Called by the channel preset <select> onchange handler.
window.toggleCustomInput = function(pid, val){
  var wrap = document.getElementById('chCustomWrap_' + pid);
  var input = document.getElementById('chCustomInput_' + pid);
  if(!wrap){ return; }
  var show = val === '__custom__';
  wrap.style.display = show ? '' : 'none';
  if(input){ input.required = show; }
};
</script>`, t(lang, "保存中...", "Saving..."), requiredPrompt, authorizingText)
}

func authNoticeHTML(lang, title, body string) string {
	return fmt.Sprintf(`<div class="section-card glass"><h3>%s</h3><p class="hint">%s</p></div>`, title, body)
}

func appShell(title, subtitle, lang string, r *http.Request, nav string, body string) string {
	subHTML := ""
	if subtitle != "" {
		subHTML = `<div class="brand-sub">` + subtitle + `</div>`
	}
	homeHref := appendLang("/bot/admin", lang)
	if r != nil {
		homeHref = withLang("/bot/admin", r)
	}
	navHTML := ""
	if nav != "" {
		navHTML = `<nav class="primary-nav" aria-label="Primary navigation">` + nav + `</nav>`
	}
	return fmt.Sprintf(`<!doctype html>
<html lang="%s">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>%s</style>
%s
</head>
<body>
<a class="skip-link" href="#main-content">%s</a>
<div class="shell">
  <div class="topbar">
    <a class="brand-block" href="%s" aria-label="KitsuSync home">
      <div>
        <div class="eyebrow">Kitsu x Discord</div>
        <div class="brand-title">%s</div>
        %s
      </div>
    </a>
    <div class="top-actions">
      %s
      %s
    </div>
  </div>
  <main id="main-content">
  <div id="ui-status" class="sr-only" aria-live="polite" aria-atomic="true"></div>
  %s
  </main>
</div>
</body>
</html>`, lang, title, adminThemeCSS, shellHeadExtras(), t(lang, "本文へ移動", "Skip to content"), homeHref, title, subHTML, navHTML, langToggleHTML(r, lang), body)
}
