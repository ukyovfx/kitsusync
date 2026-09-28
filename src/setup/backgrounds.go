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
  const pointer={x:-10000,y:-10000,target:0,amount:0};
  let width=0,height=0,dpr=1,last=0,frame=0,time=0,cardCenterX=0,cardCenterY=0;
  const mix=(a,b,t)=>a+(b-a)*t;
  const clamp=(value,min,max)=>Math.max(min,Math.min(max,value));
  const smoothstep=(a,b,value)=>{const t=clamp((value-a)/(b-a),0,1);return t*t*(3-2*t)};
  const quintic=value=>{const t=clamp(value,0,1);return t*t*t*(t*(t*6-15)+10)};
  const rgba=(r,g,b,a)=>'rgba('+Math.round(r)+','+Math.round(g)+','+Math.round(b)+','+a.toFixed(3)+')';
  const palette=value=>{
    const stops=[[92,35,16],[232,90,26],[255,190,86]];
    const t=clamp(value,0,1);
    if(t<.55){const p=t/.55;return stops[0].map((v,i)=>mix(v,stops[1][i],p));}
    const p=(t-.55)/.45;return stops[1].map((v,i)=>mix(v,stops[2][i],p));
  };
  const clothColors=[
    'rgba(34,16,11,.06)','rgba(58,21,10,.10)','rgba(82,26,8,.15)','rgba(118,38,10,.21)',
    'rgba(156,52,14,.28)','rgba(196,68,18,.38)','rgba(228,84,21,.48)','rgba(248,102,26,.60)',
    'rgba(255,126,34,.72)','rgba(255,190,86,.84)'
  ];
  const maxDotsPerTier=12000;
  const clothBuckets=Array.from({length:10},()=>({coords:new Float32Array(maxDotsPerTier*3),count:0}));
  const resize=()=>{
    dpr=Math.min(window.devicePixelRatio||1,1.5);
    width=document.documentElement.clientWidth;height=window.innerHeight;
    canvas.width=Math.round(width*dpr);canvas.height=Math.round(height*dpr);
    canvas.style.width=width+'px';canvas.style.height=height+'px';
    ctx.setTransform(dpr,0,0,dpr,0,0);
    draw(mode==='login-fabric'?time:0);
  };
  const evaluateSideClothProfile=(side,colX,seconds,u)=>{
    const isLeft=side==='left';
    const flowDir=isLeft?1:-1;
    const travelTime=seconds*.36*flowDir;
    const centerX=cardCenterX||width*.5;
    const mirroredX=isLeft?centerX-colX:colX-centerX;
    const outerDist=isLeft?colX:width-colX;
    const outerRamp=clamp((outerDist+140)/400,0,1);
    const outerTaperFactor=.74+.26*Math.pow(smoothstep(0,1,outerRamp),.95);
    const funnelApproach=Math.pow(Math.max(0,(u-.20)/.80),1.65);
    const centerConvergence=Math.min(1,funnelApproach);
    const mobile=width<600;
    const sheetWidth=mobile?96:Math.min(180,height*.25);
    const centerMinWidth=mobile?24:36;
    const widthFunnelRatio=Math.max(.24,1-centerConvergence*(1-centerMinWidth/sheetWidth));
    const effectiveWidth=sheetWidth*widthFunnelRatio*outerTaperFactor;
    const verticalFunnelEnvelope=Math.pow(Math.max(.22,1-centerConvergence*.78),1.15);
    const dx=Math.abs(colX-pointer.x);
    const gust=smoothstep(1,0,dx/220);
    const cursorGust=mobile?.13:.35;
    const cursorGustAmp=1+gust*cursorGust*pointer.amount;
    const warpW=1.2;
    const phaseOffset=isLeft?0:(mobile?.15:.35);
    const warpedX=mirroredX+(Math.sin(mirroredX*.00118+travelTime*.30+phaseOffset)*56+Math.cos(mirroredX*.00270-travelTime*.20+phaseOffset)*24)*warpW;
    const env1=Math.sin(warpedX*.00125-travelTime*.25+phaseOffset);
    const env2=Math.cos(warpedX*.00260+travelTime*.17+phaseOffset*.7);
    const waveHeightEnv=Math.max(.25,1+(env1*.48+env2*.25)*(mobile?.68:1.25));
    const macroTwist=mobile?.58:1.15;
    const macroWaveY=(Math.sin(warpedX*.00070-travelTime*.15+phaseOffset)*28+Math.cos(warpedX*.00140+phaseOffset)*12)*macroTwist;
    const macroWaveZ=(Math.cos(warpedX*.00075-travelTime*.12+phaseOffset)*50+Math.sin(warpedX*.00150+phaseOffset)*22)*macroTwist;
    const phiM1=warpedX*.00205-travelTime*.65+phaseOffset;
    const phiM2=warpedX*.00415+travelTime*.38+.5+phaseOffset;
    const stokes=Math.sin(phiM1)+.36*Math.sin(2*phiM1-.36)-.12*Math.cos(3*phiM1);
    const stokesSub=(Math.cos(phiM2)+.22*Math.sin(2*phiM2))*.46;
    const silhouetteScale=mobile?.62:1.15;
    const profileWave=(stokes*42+stokesSub*20)*waveHeightEnv*silhouetteScale;
    const fineFlutter=mobile?.62:1.7;
    const microP1=warpedX*.024-travelTime*1.8+.4+phaseOffset;
    const microP2=warpedX*.048+travelTime*2.3;
    const microFlutterY=(Math.sin(microP1)*3.6+Math.cos(microP2)*1.6)*fineFlutter;
    const microFlutterZ=(Math.cos(microP1*1.2)*4.2+Math.sin(microP2*.8)*2)*fineFlutter;
    const elevation=(macroWaveY+profileWave+microFlutterY)*verticalFunnelEnvelope*cursorGustAmp;
    const spineY=cardCenterY+elevation+(isLeft?-4:3)*Math.pow(Math.max(0,1-u),1.25);
    const phiD=warpedX*.00185-travelTime*.48+phaseOffset;
    const spineZ=(macroWaveZ+Math.sin(phiD)*70+Math.cos(warpedX*.0037+travelTime*.28+phaseOffset)*34+microFlutterZ)*(.85+env1*.28)*verticalFunnelEnvelope*cursorGustAmp;
    const waveSlope=Math.cos(phiM1)*.60-Math.sin(phiM2)*.30;
    const rollTorsion=mobile?.56:1.25;
    const dynamicTwist=(Math.sin(warpedX*.00175-travelTime*.50+phaseOffset+.25)*.62+waveSlope*.34)*rollTorsion*verticalFunnelEnvelope;
    const obliqueAngle=20*Math.PI/180;
    const totalRoll=obliqueAngle+dynamicTwist;
    return {spineY,spineZ,totalRoll,effectiveWidth,travelTime,verticalFunnelEnvelope,centerConvergence,cursorGustAmp,outerTaperFactor,warpedX,env1,gust};
  };
  const collectSurfaceVertices=(seconds)=>{
    const mobile=width<600;
    const centerX=width*.5;
    const card=document.querySelector('.login-card');
    if(!card)return [];
    const rect=card.getBoundingClientRect();
    const rowCount=mobile?13:20;
    const spacingX=mobile?15:Math.max(8,width>1700?10:9);
    const buckets=clothBuckets;
    for(const bucket of buckets)bucket.count=0;
    const focalLength=720;
    const sides=['left','right'];
    for(const side of sides){
      const isLeft=side==='left';
      const step=isLeft?spacingX:-spacingX;
      const end=isLeft?centerX+75:centerX-75;
      let colIndex=0;
      for(let colX=isLeft?-140:width+140;isLeft?colX<=end:colX>=end;colX+=step,colIndex++){
        const distToCenter=Math.abs(colX-centerX);
        const u=clamp(1-distToCenter/(centerX+140),0,1);
        const profile=evaluateSideClothProfile(side,colX,seconds,u);
        const halfW=profile.effectiveWidth*.5;
        const roll=profile.totalRoll,cosR=Math.cos(roll),sinR=Math.sin(roll);
        const facingFactor=Math.abs(sinR);
        const edgeDist=isLeft?colX+160:width+160-colX;
        const edgeDissolve=quintic(edgeDist/260);
        const baseDissolve=edgeDissolve;
        if(baseDissolve<=.002)continue;
        const foldCompression=1/Math.sqrt(.30+cosR*cosR);
        const foldHighlight=clamp(foldCompression*.95,.65,2.1)*1.2;
        for(let row=0;row<rowCount;row++){
          const rawV=rowCount>1?(row/(rowCount-1))*2-1:0;
          const v=Math.sign(rawV)*Math.pow(Math.abs(rawV),.94);
          const s=v*halfW;
          const drapeCamber=mobile?.48:.88;
          const catenaryZ=(1-v*v*.85)*(halfW*.32)*drapeCamber;
          const sCurveY=v*(1-v*v)*(halfW*.20)*drapeCamber;
          const dynamicCatenaryZ=(catenaryZ*Math.sin(profile.warpedX*.0026-profile.travelTime*.55)+sCurveY*Math.cos(profile.warpedX*.0036+profile.travelTime*.62))*profile.verticalFunnelEnvelope*profile.cursorGustAmp;
          const edgeWeight=Math.pow(Math.abs(v),1.55);
          const edgeFlutterPhase=profile.warpedX*.0088-profile.travelTime*1.32+v*2.3;
          const edgeFlutterScale=mobile?.48:1.15;
          const flutterY=edgeWeight*Math.sin(edgeFlutterPhase)*(5.5*edgeFlutterScale)*profile.verticalFunnelEnvelope;
          const flutterZ=edgeWeight*Math.cos(edgeFlutterPhase*.85)*(9.5*edgeFlutterScale)*profile.verticalFunnelEnvelope;
          const strandMicroPhase=profile.warpedX*.034-profile.travelTime*2.1+v*4.6;
          const microFlutterScale=mobile?.62:1.7;
          const strandMicroY=Math.sin(strandMicroPhase)*(2*microFlutterScale)*profile.verticalFunnelEnvelope;
          const strandMicroZ=Math.cos(strandMicroPhase*1.25)*(3*microFlutterScale)*profile.verticalFunnelEnvelope;
          const depthWavePhase=profile.warpedX*.0072-profile.travelTime*1.25+v*Math.PI;
          const fineDepthScale=mobile?.38:1.2;
          const fineDepthZ=(Math.sin(depthWavePhase)*22+Math.cos(depthWavePhase*1.8+.5)*10)*fineDepthScale*profile.verticalFunnelEnvelope;
          const fineDepthY=Math.cos(depthWavePhase)*7.5*fineDepthScale*profile.verticalFunnelEnvelope;
          const dYLocal=sCurveY+flutterY+strandMicroY+fineDepthY;
          const dZLocal=dynamicCatenaryZ+flutterZ+strandMicroZ+fineDepthZ;
          const deltaY=s*sinR+dYLocal*cosR-dZLocal*sinR;
          const deltaZ=s*cosR+dYLocal*sinR+dZLocal*cosR;
          const worldX=colX+deltaZ*sinR*.06;
          const worldY=profile.spineY+deltaY;
          const worldZ=profile.spineZ+deltaZ;
          const normDepth=clamp((160-worldZ)/320,0,1);
          const strandSeed=((Math.sin(colIndex*12.9898+row*78.233)*43758.5453)%1+1)%1;
          const depthGate=clamp((normDepth-.04)/.001,0,1);
          const strandDissolve=1-smoothstep(strandSeed-.22,strandSeed+.06,depthGate);
          const strandVisibility=.15+.85*(1-strandDissolve);
          const scale=focalLength/(focalLength+worldZ);
          const projectedX=colX+(worldX-colX)*scale;
          const projectedY=cardCenterY+(worldY-cardCenterY)*scale;
          if(projectedX< -8||projectedX>width+8||projectedY< -8||projectedY>height+8)continue;
          if(projectedX>=rect.left-7&&projectedX<=rect.right+7&&projectedY>=rect.top-9&&projectedY<=rect.bottom+9)continue;
          const cardEdge=isLeft?rect.left:rect.right;
          const cardDistance=mobile?rect.top-projectedY:(isLeft?cardEdge-projectedX:projectedX-cardEdge);
          const cardNorm=clamp((cardDistance-(mobile?3:10))/(mobile?54:155),0,1);
          const cardDissolve=quintic(cardNorm);
          const effectiveDissolve=baseDissolve*strandVisibility*cardDissolve;
          if(effectiveDissolve<=.003)continue;
          const normElevation=clamp((height*.5+75-worldY)/150,0,1);
          const prominence=clamp(normDepth*.76+normElevation*.24+profile.gust*pointer.amount*.16,0,1);
          const foldBoost=Math.max(0,(foldHighlight-.90)*.42);
          const facingBoost=facingFactor*.26;
          const sizeMultiplier=.65+Math.pow(prominence,2.6)*(mobile?2.05:2.60)+foldBoost*.25+facingBoost+profile.centerConvergence*.30;
          const dissolveScale=Math.pow(effectiveDissolve,1.35);
          const gustScale=1+profile.gust*pointer.amount*(mobile?.18:.35);
          const radius=(mobile?.66:.85)*sizeMultiplier*(.74+.26*profile.outerTaperFactor)*dissolveScale*gustScale;
          if(radius<(mobile?.10:.12))continue;
          const sheen=Math.abs(deltaY)/(halfW+1);
          const colorMetric=(prominence*.68+foldBoost*.20+sheen*.12+profile.centerConvergence*.10+profile.gust*pointer.amount*.10)*Math.pow(effectiveDissolve,.75);
          const tierIndex=Math.min(9,Math.floor(clamp(colorMetric*10,0,9.99)));
          const bucket=buckets[tierIndex];
          if(bucket.count>=maxDotsPerTier)continue;
          const offset=bucket.count*3;
          bucket.coords[offset]=projectedX;
          bucket.coords[offset+1]=projectedY;
          bucket.coords[offset+2]=radius;
          bucket.count++;
        }
      }
    }
    return buckets;
  };
  const drawLogin=seconds=>{
    ctx.clearRect(0,0,width,height);
    const card=document.querySelector('.login-card');
    if(!card)return;
    const rect=card.getBoundingClientRect();
    const mobile=width<600;
    cardCenterX=rect.left+rect.width*.5;
    cardCenterY=mobile?rect.top-42:height*.5;
    const buckets=collectSurfaceVertices(seconds);
    const pi2=Math.PI*2;
    for(let tierIndex=0;tierIndex<buckets.length;tierIndex++){
      const bucket=buckets[tierIndex];
      if(!bucket.count)continue;
      ctx.fillStyle=clothColors[tierIndex];
      ctx.beginPath();
      for(let i=0;i<bucket.count*3;i+=3){
        const x=bucket.coords[i],y=bucket.coords[i+1],radius=bucket.coords[i+2];
        ctx.moveTo(x+radius,y);ctx.arc(x,y,radius,0,pi2);
      }
      ctx.fill();
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
  const draw=seconds=>{if(mode==='login-fabric')drawLogin(seconds);else drawDots(seconds)};
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
