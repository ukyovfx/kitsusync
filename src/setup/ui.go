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
  align-items:flex-start;
  margin-bottom:18px;
}
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
  min-width:104px;
  padding:4px;
  border-radius:999px;
}
.lang-thumb{
  position:absolute;
  top:4px;
  bottom:4px;
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
  text-align:center;
  font-size:10px;
  padding:7px 10px;
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
.hint,.muted{color:var(--muted);line-height:1.7}
.form-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(210px,1fr));gap:9px;}
.form-span-2{grid-column:span 2}
label{display:block;margin:0 0 8px;color:#ddd8d0;font-size:12px;text-transform:uppercase;letter-spacing:.16em;font-family:"Space Grotesk","Outfit",sans-serif;}
input,select,textarea{width:100%;min-width:0;border-radius:14px;border:1px solid rgba(255,255,255,.12);background:rgba(8,8,10,.7);color:var(--text);padding:10px 12px;outline:none;transition:border-color .18s ease, box-shadow .18s ease, background .18s ease;}
input:focus,select:focus,textarea:focus{border-color:rgba(255,141,72,.72);box-shadow:0 0 0 3px rgba(232,90,26,.16);}
button:focus-visible,a:focus-visible,input:focus-visible,select:focus-visible,textarea:focus-visible{outline:3px solid #6dc3ff;outline-offset:3px;}
input[readonly],input[disabled],select[disabled]{opacity:.72;cursor:not-allowed;}
.field-help{color:var(--muted-2);font-size:12px;margin-top:6px;line-height:1.55;}
.button-row{display:flex;gap:6px;align-items:center;flex-wrap:wrap;margin-top:12px;}
.btn,.btn-sm,.btn-ghost,.btn-danger{border:none;border-radius:999px;padding:8px 14px;cursor:pointer;font-weight:600;font-family:"Space Grotesk","Outfit",sans-serif;letter-spacing:.04em;transition:transform .18s ease, opacity .18s ease, box-shadow .18s ease;}
.btn:hover,.btn-sm:hover,.btn-ghost:hover,.btn-danger:hover{transform:translateY(-1px)}
.btn:disabled,.btn-sm:disabled,.btn-ghost:disabled,.btn-danger:disabled{cursor:not-allowed;opacity:.72;transform:none}
.btn{color:#140904;background:linear-gradient(135deg, var(--accent), var(--accent-2));box-shadow:0 14px 30px rgba(232,90,26,.24);}
.btn-sm{color:#140904;background:linear-gradient(135deg, rgba(255,141,72,.94), rgba(232,90,26,.9));padding:6px 10px;}
.btn-ghost{color:var(--text);background:rgba(255,255,255,.06);border:1px solid rgba(255,255,255,.12);}
.btn-danger{color:#fff5f2;background:rgba(255,106,80,.18);border:1px solid rgba(255,106,80,.3);}
.status-pill,.tag{display:inline-flex;align-items:center;gap:4px;padding:5px 8px;border-radius:999px;background:rgba(255,255,255,.05);border:1px solid rgba(255,255,255,.08);color:var(--muted);font-size:12px;line-height:1.25;}
.status-pill{white-space:nowrap;}
.status-pill.ok{color:#d7f4d4;border-color:rgba(142,207,139,.28);background:rgba(142,207,139,.08)}
.status-pill.warn{color:#fff1c4;border-color:rgba(255,200,80,.3);background:rgba(255,200,80,.1)}
.status-pill.bad{color:#ffd3ca;border-color:rgba(255,106,80,.28);background:rgba(255,106,80,.08)}
.table-wrap{overflow:auto;border-radius:16px;border:1px solid rgba(255,255,255,.08);background:rgba(4,4,6,.35);}
table{width:100%;border-collapse:collapse}
th,td{padding:9px 10px;border-bottom:1px solid rgba(255,255,255,.07);text-align:left;vertical-align:top;overflow-wrap:anywhere;word-break:break-word}
th{color:var(--muted-2);font-size:12px;text-transform:uppercase;letter-spacing:.16em;font-family:"Space Grotesk","Outfit",sans-serif;font-weight:500;}
code{background:rgba(255,255,255,.06);padding:4px 8px;border-radius:10px;color:#fff7f0;}
.empty{text-align:center;padding:18px 12px;border-radius:16px;border:1px dashed rgba(255,255,255,.16);background:rgba(255,255,255,.03);}
.metric-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:9px;}
.metric-card{border-radius:16px;padding:12px;background:rgba(255,255,255,.04);border:1px solid rgba(255,255,255,.08);}
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
.step-connector{flex:1;height:2px;background:rgba(255,255,255,.1);margin:0 12px;min-width:24px}
.workflow-overview{padding:16px;background:linear-gradient(180deg, rgba(255,255,255,.08), rgba(255,255,255,.03)),linear-gradient(135deg, rgba(109,195,255,.06), rgba(232,90,26,.05) 64%, transparent);}
.workflow-overview-head{margin-bottom:8px}
.workflow-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:9px;margin-top:9px}
.workflow-card{border-radius:16px;padding:12px 12px 13px;background:rgba(255,255,255,.04);border:1px solid rgba(255,255,255,.08);display:grid;gap:8px;align-content:start;min-height:158px}
.workflow-card h3{margin:0;font-size:14px;line-height:1.2}
.workflow-card p{margin:0}
.workflow-card-head{display:flex;justify-content:space-between;align-items:center;gap:10px;flex-wrap:wrap}
.workflow-step-badge{width:24px;height:24px;border-radius:8px;display:grid;place-items:center;font-weight:700;font-family:"Space Grotesk","Outfit",sans-serif;background:rgba(255,255,255,.08);border:1px solid rgba(255,255,255,.1)}
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
@media (max-width:900px){.topbar{flex-direction:column}.top-actions{width:100%;justify-content:flex-start}.page-card{padding:14px}.page-heading{flex-direction:column}.dashboard-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.workflow-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}
@media (max-width:640px){.shell{padding:12px 9px 32px}.topbar{gap:10px}.top-actions{gap:6px}.brand-title{font-size:21px}.page-heading h1{font-size:22px}.page-card{width:auto!important;max-width:100%!important;margin-left:0!important;margin-right:0!important}.section-card{padding:12px;min-width:0}.dashboard-grid,.metric-grid,.form-grid,.workflow-grid{grid-template-columns:1fr}.form-span-2{grid-column:auto}.nav-card{width:100%}.lang-toggle{min-width:100px}.btn,.btn-sm,.btn-ghost,.btn-danger{max-width:100%;white-space:normal}.accordion{border-radius:16px}.accordion summary{padding:11px}.accordion-body{padding:0 9px 11px}.accordion-summary-main{flex-basis:100%}.accordion-summary-side{width:100%;justify-content:flex-start}.accordion-trigger{width:100%;justify-content:space-between}.project-panel-head{flex-direction:column}.project-panel-meta{width:100%;gap:8px}.project-panel-meta .tag{max-width:100%;white-space:normal}.project-panel-meta form,.project-panel-meta .btn-danger{width:100%}.table-wrap{margin:0 -2px}.project-channel-table{overflow:visible;background:transparent;border:none}.project-channel-table thead{display:none}.project-channel-table table,.project-channel-table tbody,.project-channel-table tr,.project-channel-table td{display:block;width:100%}.project-channel-table tr{margin:0 0 8px;padding:8px;border:1px solid rgba(255,255,255,.08);border-radius:14px;background:rgba(255,255,255,.035)}.project-channel-table td{padding:5px 0;border-bottom:none}.project-channel-table td::before{content:attr(data-label);display:block;margin-bottom:4px;color:var(--muted-2);font-size:10px;text-transform:uppercase;letter-spacing:.14em;font-family:"Space Grotesk","Outfit",sans-serif}.project-channel-table td:last-child{white-space:normal}.project-channel-table .delete-form{justify-content:stretch}.project-channel-table .delete-form .btn-danger{width:100%}.delete-modal{padding:9px}.delete-box{padding:12px;border-radius:15px}.metric-value-host{font-size:14px}.workflow-overview{padding:12px}.workflow-card{min-height:auto}.setup-steps{flex-wrap:wrap;row-gap:9px}}
@media (max-width:480px){.section-card{padding:9px;box-shadow:none}.glass{box-shadow:none}.shell{padding:7px 6px 28px}.hint{font-size:.76rem}.metric-label{font-size:.7rem}.metric-value{font-size:1.05rem}.section-card h3{font-size:.92rem}.table-wrap table{font-size:.75rem}.field-help,.field-label{font-size:.76rem}}
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
</script>`
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
	requiredPrompt := t(lang, "「削除」と入力してください", `Type "delete" to confirm`)
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
    if(form.classList.contains('delete-form')){ return; }
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
		navHTML = `<nav aria-label="Primary navigation">` + nav + `</nav>`
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
    </div>
  </div>
  %s
  <main id="main-content">
  %s
  </main>
</div>
</body>
</html>`, lang, title, adminThemeCSS, shellHeadExtras(), homeHref, title, subHTML, langToggleHTML(r, lang), navHTML, body)
}
