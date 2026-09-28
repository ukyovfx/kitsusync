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
  const cursorRadius=220,flowSpeed=.1;
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
    const mobile=width<600;
    const centerY=mobile?rect.top-12:rect.top+rect.height*.5;
    const maxHalfHeight=Math.min(height*(mobile?.05:.14),mobile?38:92);
    const columns=mobile?Math.max(24,Math.ceil(width/12)):Math.max(80,Math.ceil(width/8));
    const rows=mobile?8:17;
    const ribbonLayers=mobile?2:3;
    const smoothstep=(a,b,x)=>{const v=Math.max(0,Math.min(1,(x-a)/(b-a)));return v*v*(3-2*v)};
    for(let layer=0;layer<ribbonLayers;layer++){
      const layerDepth=(layer+1)/(ribbonLayers+1);
      const layerOffset=(layer-(ribbonLayers-1)*.5)*(mobile?2:6);
      for(let col=0;col<=columns;col++){
        const u=col/columns;
        const xBase=u*width;
        const edgeTaper=smoothstep(0,.08,u)*smoothstep(0,.08,1-u);
        const centerNarrowing=1-.18*Math.exp(-Math.pow((u-.5)/.18,2));
        const halfHeight=maxHalfHeight*edgeTaper*centerNarrowing;
        if(halfHeight<1.5)continue;
        const wavePhase=u*5.2+seconds*flowSpeed+layer*.71;
        const xNoise=Math.sin(col*2.31+layer*1.77)*4;
        const gustDX=pointer.x-xBase,gustDY=pointer.y-centerY;
        const gust=Math.exp(-(gustDX*gustDX+gustDY*gustDY)/(2*cursorRadius*cursorRadius))*pointer.amount;
        for(let row=0;row<rows;row++){
          const q=row/(rows-1)*2-1;
          const slowAmplitude=(mobile?6:21)*(0.72+layerDepth*.28)*(1+gust*.22);
          const slowWave=Math.sin(wavePhase+Math.sin(seconds*.12+u*2+q)*.6)*slowAmplitude;
          const mediumWave=Math.sin(wavePhase*2.35+q*3.8+layer*.93)*(mobile?3.5:11);
          const flutter=Math.sin(u*width*(mobile?.055:.075)+q*17+seconds*1.7+layer*.83)*(mobile?1:1.5);
          const twist=Math.sin(wavePhase+q*2.6+layer*.63)*(mobile?3.5:8);
          const x=Math.max(0,Math.min(width,xBase+q*twist+xNoise));
          const unliftedY=centerY+q*halfHeight+slowWave+mediumWave+flutter+layerOffset;
          const dx=pointer.x-x,dy=pointer.y-unliftedY;
          const local=Math.exp(-(dx*dx+dy*dy)/(2*cursorRadius*cursorRadius))*pointer.amount;
          const lift=local*(mobile?3.5:13);
          const y=unliftedY-lift;
          const depth=.5+.5*Math.sin(wavePhase+q*2.7+layer*1.3);
          const insideCard=x>=rect.left-10&&x<=rect.right+10&&y>=rect.top-10&&y<=rect.bottom+10;
          const cardAttenuation=insideCard?.78:1;
          const organic=.025*Math.sin(col*12.989+row*78.233+layer*4.21);
          const alpha=(.075+depth*.19+local*.09+organic)*edgeTaper*cardAttenuation*(mobile?.58:1);
          if(alpha<(mobile?.012:.025))continue;
          const mirroredCenter=1-Math.abs(u-.5)*2;
          const color=palette(.12+mirroredCenter*.62+depth*.2+organic);
          const size=(.42+depth*.76+layerDepth*.18+local*.24)*(mobile?.76:1);
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
