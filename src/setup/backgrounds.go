package setup

import "html/template"

func backgroundForSurface(surface string) (template.HTML, template.HTML) {
	mode := ""
	switch surface {
	case "login-surface":
		mode = "login-fabric"
	case "admin-surface":
		mode = "app-dots"
	default:
		return "", ""
	}
	markup := template.HTML(`<canvas class="background-canvas" data-background="` + mode + `" aria-hidden="true"></canvas>`)
	return markup, template.HTML(backgroundCanvasScript)
}

const backgroundCanvasScript = `<script>
(()=>{
  const canvas=document.querySelector('[data-background]');
  if(!canvas)return;
  const ctx=canvas.getContext('2d',{alpha:true});
  if(!ctx)return;
  const mode=canvas.dataset.background;
  const reduced=matchMedia('(prefers-reduced-motion: reduce)');
  const meshRows=26,spacingX=7,sheetWidth=180,centerMinWidth=36,cursorRadius=220,flowSpeed=.1,foldBrightness=1.2,oblique=Math.tan(20*Math.PI/180);
  const pointer={x:-10000,y:-10000,target:0,amount:0};
  let width=0,height=0,dpr=1,last=0,frame=0,time=0;
  const resize=()=>{
    dpr=Math.min(window.devicePixelRatio||1,1.5);
    width=document.documentElement.clientWidth;height=window.innerHeight;
    canvas.width=Math.round(width*dpr);canvas.height=Math.round(height*dpr);
    canvas.style.width=width+'px';canvas.style.height=height+'px';
    ctx.setTransform(dpr,0,0,dpr,0,0);
    draw(0);
  };
  const mix=(a,b,t)=>a+(b-a)*t;
  const rgba=(r,g,b,a)=>` + "`rgba(${Math.round(r)},${Math.round(g)},${Math.round(b)},${a.toFixed(3)})`" + `;
  const palette=t=>{
    const stops=[[92,35,16],[232,90,26],[255,190,86]];
    if(t<.55){const p=t/.55;return stops[0].map((v,i)=>mix(v,stops[1][i],p));}
    const p=(t-.55)/.45;return stops[1].map((v,i)=>mix(v,stops[2][i],p));
  };
  const drawLogin=seconds=>{
    ctx.clearRect(0,0,width,height);
    const card=document.querySelector('.login-card');
    if(!card)return;
    const rect=card.getBoundingClientRect();
    const centerY=rect.top+rect.height*.5;
    const innerGap=width<600?4:18;
    const leftEdge=Math.max(0,rect.left-innerGap);
    const rightEdge=Math.min(width,rect.right+innerGap);
    const band=Math.min(sheetWidth,leftEdge,width-rightEdge);
    if(band<3)return;
    const fields=[
      {start:leftEdge-band,end:leftEdge,direction:1},
      {start:rightEdge,end:rightEdge+band,direction:-1},
    ];
    for(const field of fields){
      const start=field.direction===1?field.end-band:field.start;
      const columns=Math.max(2,Math.floor(band/spacingX));
      for(let col=0;col<=columns;col++){
        const u=col/columns;
        const xBase=start+u*band;
        const inward=field.direction===1?u:1-u;
        const taper=inward*inward*(3-2*inward);
        const halfHeight=mix(height*.475,centerMinWidth*.5,taper);
        const depthPhase=(inward*band*.012+seconds*flowSpeed);
        for(let row=0;row<meshRows;row++){
          const v=row/(meshRows-1);
          const baseY=centerY+(v-.5)*halfHeight*2;
          const gustDX=pointer.x-xBase,gustDY=pointer.y-baseY;
          const influence=Math.exp(-(gustDX*gustDX+gustDY*gustDY)/(2*cursorRadius*cursorRadius))*pointer.amount*.35;
          const wavePhase=depthPhase+(v*6.2)+.9;
          const waveAmp=mix(25,6,taper)*(1+influence);
          const slowWave=Math.sin(wavePhase+Math.sin(seconds*.12+v*2)*.6)*waveAmp;
          const flutter=Math.sin(inward*band*.075+v*18+seconds*1.7+field.direction*.16)*1.7;
          const skew=(v-.5)*Math.min(band*.52,height*oblique);
          const x=Math.min(field.end,Math.max(field.start,xBase+field.direction*(skew+Math.sin(wavePhase*.75)*5+Math.sin(seconds*.18+v*4)*2)));
          const y=baseY+slowWave+flutter;
          const dx=pointer.x-x,dy=pointer.y-y;
          const local=Math.exp(-(dx*dx+dy*dy)/(2*cursorRadius*cursorRadius))*pointer.amount*.35;
          const depth=.5+.5*Math.sin(depthPhase+v*3.4);
          const envelope=Math.min(1,Math.max(0,(halfHeight-Math.abs(y-centerY)+24)/32));
          const fade=Math.min(1,Math.max(0,(1-inward)/.12));
          const alpha=((.08+depth*.32)*foldBrightness+local*.11)*envelope*fade;
          if(alpha<.025)continue;
          const color=palette(mix(.06,.96,inward)*.78+depth*.22);
          const size=.48+depth*.9+taper*.16;
          ctx.fillStyle=rgba(color[0],color[1],color[2],alpha);
          ctx.beginPath();ctx.arc(x,y,size,0,Math.PI*2);ctx.fill();
        }
      }
    }
  };
  const drawDots=seconds=>{
    ctx.clearRect(0,0,width,height);
    const spacing=34;
    for(let y=18;y<height;y+=spacing){
      for(let x=18;x<width;x+=spacing){
        const dx=pointer.x-x,dy=pointer.y-y;
        const local=Math.exp(-(dx*dx+dy*dy)/(2*150*150))*pointer.amount;
        const slow=Math.sin(x*.003+y*.004+seconds*.09)*.5+.5;
        const alpha=.032+slow*.018+local*.034;
        const radius=.85+slow*.12+local*.22;
        ctx.fillStyle=rgba(203+slow*31,112+slow*48,68+slow*34,alpha);
        ctx.beginPath();ctx.arc(x,y,radius,0,Math.PI*2);ctx.fill();
      }
    }
  };
  const draw=seconds=>{
    if(mode==='login-fabric')drawLogin(seconds);else drawDots(seconds);
  };
  const tick=now=>{
    frame=0;
    if(document.hidden)return;
    if(now-last<33){frame=requestAnimationFrame(tick);return;}
    const elapsed=Math.min(.05,(now-last)/1000||0);
    last=now;time+=elapsed;
    pointer.amount+=(pointer.target-pointer.amount)*(1-Math.pow(.94,elapsed*60));
    draw(time);
    if(!reduced.matches)frame=requestAnimationFrame(tick);
  };
  const start=()=>{
    if(frame)cancelAnimationFrame(frame);
    frame=0;last=0;
    draw(time);
    if(!reduced.matches&&!document.hidden)frame=requestAnimationFrame(tick);
  };
  window.addEventListener('pointermove',event=>{pointer.x=event.clientX;pointer.y=event.clientY;pointer.target=1;},{passive:true});
  window.addEventListener('pointerleave',()=>{pointer.target=0;},{passive:true});
  document.addEventListener('visibilitychange',start);
  window.addEventListener('resize',resize,{passive:true});
  reduced.addEventListener?.('change',start);
  resize();start();
})();
  </script>`
