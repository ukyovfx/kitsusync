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
  let width=0,height=0,dpr=1,last=0,frame=0,time=0,centerX=0,centerY=0,params;
  const desktopBaseline={meshRows:26,meshSpacingX:7.0,sheetWidth:180,centerMinWidth:36,convergenceStrength:1.00,outerTaper:.95,obliqueAngleDeg:20.0,dotSize:.85,depthContrast:5.00,backgroundSparsity:0.00,foldBrightness:1.20,microFlutter:1.70,flowSpeed:.30,silhouetteAmp:1.15,heightEnvelope:1.25,intervalWarp:1.20,rollTorsion:1.25,macroTwist:1.15,fineDepthWave:1.20,edgeFlutter:1.15,drapeCamber:.88,cursorGust:.35,cursorRadius:220,cursorRecovery:.06};
  const tierColors=[
    'rgba(18,10,8,.08)','rgba(46,16,6,.18)','rgba(82,26,8,.34)','rgba(118,38,10,.50)',
    'rgba(156,52,14,.68)','rgba(196,68,18,.82)','rgba(228,84,21,.92)','rgba(248,102,26,.97)',
    'rgba(255,126,34,1)','rgba(255,150,48,1)'
  ];
  const maxDotsPerTier=36000;
  const clothBuckets=Array.from({length:10},()=>({coords:new Float32Array(maxDotsPerTier*3),count:0}));
  const clamp=(value,min,max)=>Math.max(min,Math.min(max,value));
  const smoothstep=(a,b,value)=>{const t=clamp((value-a)/(b-a),0,1);return t*t*(3-2*t)};
  const quintic=value=>{const t=clamp(value,0,1);return t*t*t*(t*(t*6-15)+10)};
  const rgba=(r,g,b,a)=>'rgba('+Math.round(r)+','+Math.round(g)+','+Math.round(b)+','+a.toFixed(3)+')';
  const resize=()=>{
    dpr=Math.min(window.devicePixelRatio||1,1.5);
    width=document.documentElement.clientWidth;height=window.innerHeight;
    canvas.width=Math.round(width*dpr);canvas.height=Math.round(height*dpr);
    canvas.style.width=width+'px';canvas.style.height=height+'px';
    ctx.setTransform(dpr,0,0,dpr,0,0);
    draw(mode==='login-fabric'?time:0);
  };
  const evaluateSideClothProfile=(side,colX,seconds,u,pointerGust)=>{
    const isLeft=side==='left';
    const flowDir=isLeft?1:-1;
    const travelTime=seconds*.36*flowDir;
    const distFromEdge=isLeft?colX:(width-colX);
    const outerRamp=clamp((distFromEdge+140)/400,0,1);
    const smoothOuter=outerRamp*outerRamp*(3-2*outerRamp);
    const outerTaperFactor=.74+.26*Math.pow(smoothOuter,params.outerTaper);
    const funnelApproach=Math.pow(Math.max(0,(u-.20)/.80),1.65);
    const centerConvergence=Math.min(1,funnelApproach*params.convergenceStrength);
    const widthFunnelRatio=Math.max(.24,1-centerConvergence*(1-params.centerMinWidth/params.sheetWidth));
    const effectiveWidth=params.sheetWidth*widthFunnelRatio*outerTaperFactor;
    const verticalFunnelEnvelope=Math.pow(Math.max(.22,1-centerConvergence*.78),1.15);
    const cursorGustAmp=1+pointerGust*params.cursorGust;
    let xWarp;
    if(isLeft){
      xWarp=(Math.sin(colX*.00118+travelTime*.30)*56+Math.cos(colX*.00270-travelTime*.20)*24)*params.intervalWarp;
    }else{
      xWarp=(Math.cos(colX*.00128+travelTime*.26+1.7)*52+Math.sin(colX*.00290-travelTime*.16)*26)*params.intervalWarp;
    }
    const warpedX=colX+xWarp;
    let env1,waveHeightEnv;
    if(isLeft){
      env1=Math.sin(warpedX*.00125-travelTime*.25);
      const env2=Math.cos(warpedX*.00260+travelTime*.17);
      waveHeightEnv=Math.max(.25,1+(env1*.48+env2*.25)*params.heightEnvelope);
    }else{
      env1=Math.cos(warpedX*.00135-travelTime*.22+1.9);
      const env2=Math.sin(warpedX*.00280+travelTime*.15);
      waveHeightEnv=Math.max(.25,1+(env1*.45+env2*.25)*params.heightEnvelope);
    }
    let macroWaveY,macroWaveZ;
    if(isLeft){
      macroWaveY=(Math.sin(warpedX*.00070-travelTime*.15)*28+Math.cos(warpedX*.00140)*12)*params.macroTwist;
      macroWaveZ=(Math.cos(warpedX*.00075-travelTime*.12)*50+Math.sin(warpedX*.00150)*22)*params.macroTwist;
    }else{
      macroWaveY=(Math.cos(warpedX*.00075+travelTime*.14+1.3)*26-Math.sin(warpedX*.00145)*12)*params.macroTwist;
      macroWaveZ=(Math.sin(warpedX*.00080+travelTime*.13+1.6)*48-Math.cos(warpedX*.00155)*22)*params.macroTwist;
    }
    let profileWave,phiM1,phiM2;
    if(isLeft){
      phiM1=warpedX*.00205-travelTime*.65;
      phiM2=warpedX*.00415+travelTime*.38+.5;
      const stokes=Math.sin(phiM1)+.36*Math.sin(2*phiM1-.36)-.12*Math.cos(3*phiM1);
      const stokesSub=(Math.cos(phiM2)+.22*Math.sin(2*phiM2))*.46;
      profileWave=(stokes*42+stokesSub*20)*waveHeightEnv*params.silhouetteAmp;
    }else{
      phiM1=warpedX*.00225+travelTime*.62+2.4;
      phiM2=warpedX*.00440-travelTime*.34+1.3;
      const stokes=Math.cos(phiM1)+.35*Math.sin(2*phiM1-.34)-.12*Math.cos(3*phiM1);
      const stokesSub=(Math.sin(phiM2)+.22*Math.cos(2*phiM2))*.46;
      profileWave=(stokes*40+stokesSub*20)*waveHeightEnv*params.silhouetteAmp;
    }
    const microP1=warpedX*.024-travelTime*1.8+(isLeft?.4:2.2);
    const microP2=warpedX*.048+travelTime*2.3;
    const microFlutterY=(Math.sin(microP1)*3.6+Math.cos(microP2)*1.6)*params.microFlutter;
    const microFlutterZ=(Math.cos(microP1*1.2)*4.2+Math.sin(microP2*.8)*2)*params.microFlutter;
    const totalElevationY=(macroWaveY+profileWave+microFlutterY)*verticalFunnelEnvelope*cursorGustAmp;
    const asymmetryY=(isLeft?-6:4)*Math.pow(Math.max(0,1-u),1.25);
    const spineY=centerY+totalElevationY+asymmetryY;
    let spineDepthZ;
    if(isLeft){
      const phiD=warpedX*.00185-travelTime*.48;
      spineDepthZ=(macroWaveZ+Math.sin(phiD)*70+Math.cos(warpedX*.0037+travelTime*.28)*34+microFlutterZ)*(0.85+env1*.28)*verticalFunnelEnvelope*cursorGustAmp;
    }else{
      const phiD=warpedX*.00195+travelTime*.44+1.7;
      spineDepthZ=(macroWaveZ+Math.cos(phiD)*68+Math.sin(warpedX*.0039-travelTime*.26)*32+microFlutterZ)*(0.85+env1*.28)*verticalFunnelEnvelope*cursorGustAmp;
    }
    const waveSlope=Math.cos(phiM1)*.60-Math.sin(phiM2)*.30;
    const dynamicTwist=(Math.sin(warpedX*.00175-travelTime*.50+(isLeft?.25:2.3))*.62+waveSlope*.34)*params.rollTorsion*verticalFunnelEnvelope;
    return {spineY,spineZ:spineDepthZ,totalRoll:params.obliqueAngleDeg*Math.PI/180+dynamicTwist,effectiveWidth,travelTime,verticalFunnelEnvelope,centerConvergence,cursorGustAmp,outerTaperFactor,warpedX,env1};
  };
  const collectSurfaceVertices=seconds=>{
    for(const bucket of clothBuckets)bucket.count=0;
    const mobile=width<600;
    const meshRows=mobile?13:params.meshRows;
    const spacingX=mobile?15:params.meshSpacingX;
    const streamDist=seconds*52*params.flowSpeed;
    canvas.dataset.meshRows=String(meshRows);
    canvas.dataset.meshSpacingX=String(spacingX);
    canvas.dataset.streamDistance=String(streamDist);
    let maxPointerGust=0;
    const focalLength=720;
    for(const side of ['left','right']){
      const isLeft=side==='left';
      const minK=Math.floor((-180-streamDist)/spacingX);
      const maxK=Math.ceil(((isLeft?centerX:width-centerX)+70-streamDist)/spacingX);
      for(let k=minK;k<=maxK;k++){
        const baseColX=isLeft?-140+k*spacingX+streamDist:(width+140)-(k*spacingX+streamDist);
        if(isLeft?(baseColX< -170||baseColX>centerX+50):(baseColX>width+170||baseColX<centerX-50))continue;
        const distToCenter=Math.abs(baseColX-centerX);
        const u=clamp(1-distToCenter/(centerX||1),0,1);
        const base=evaluateSideClothProfile(side,baseColX,seconds,u,0);
        const baseScale=focalLength/(focalLength+base.spineZ);
        const baseX=centerX+(baseColX-centerX)*baseScale;
        const baseY=centerY+(base.spineY-centerY)*baseScale;
        const verticalRadius=mobile?clamp(base.effectiveWidth*.48,24,58):clamp(base.effectiveWidth*.55,70,120);
        const dx=pointer.x-baseX;
        const dy=pointer.y-baseY;
        const pointerDistance=Math.sqrt((dx/params.cursorRadius)**2+(dy/verticalRadius)**2);
        const pointerGust=smoothstep(1,0,pointerDistance)*pointer.amount;
        if(pointerGust>maxPointerGust)maxPointerGust=pointerGust;
        const profile=evaluateSideClothProfile(side,baseColX,seconds,u,pointerGust);
        const halfW=profile.effectiveWidth*.5;
        const roll=profile.totalRoll,cosR=Math.cos(roll),sinR=Math.sin(roll);
        const facingFactor=Math.abs(sinR);
        const edgeDist=isLeft?baseColX+160:width+160-baseColX;
        const edgeDissolve=quintic(edgeDist/260);
        if(edgeDissolve<=.002)continue;
        const foldCompression=1/Math.sqrt(.30+cosR*cosR);
        const foldHighlight=clamp(foldCompression*.95,.65,2.1)*params.foldBrightness;
        for(let row=0;row<meshRows;row++){
          const staggerX=row%2===0?0:.5*spacingX;
          const colX=baseColX+staggerX;
          const rawV=meshRows>1?(row/(meshRows-1))*2-1:0;
          const v=Math.sign(rawV)*Math.pow(Math.abs(rawV),.94);
          const s=v*halfW;
          const catenaryZ=(1-v*v*.85)*(halfW*.32)*params.drapeCamber;
          const sCurveY=v*(1-v*v)*(halfW*.20)*params.drapeCamber;
          const dynamicCatenaryZ=(catenaryZ*Math.sin(colX*.0026-profile.travelTime*.55)+sCurveY*Math.cos(colX*.0036+profile.travelTime*.62))*profile.verticalFunnelEnvelope*profile.cursorGustAmp;
          const edgeWeight=Math.pow(Math.abs(v),1.55);
          const edgeFlutterPhase=profile.warpedX*.0088-profile.travelTime*1.32+v*2.3;
          const flutterY=edgeWeight*Math.sin(edgeFlutterPhase)*(5.5*params.edgeFlutter)*profile.verticalFunnelEnvelope*profile.cursorGustAmp;
          const flutterZ=edgeWeight*Math.cos(edgeFlutterPhase*.85)*(9.5*params.edgeFlutter)*profile.verticalFunnelEnvelope*profile.cursorGustAmp;
          const strandMicroPhase=profile.warpedX*.034-profile.travelTime*2.1+v*4.6;
          const strandMicroY=Math.sin(strandMicroPhase)*(2*params.microFlutter)*profile.verticalFunnelEnvelope*profile.cursorGustAmp;
          const strandMicroZ=Math.cos(strandMicroPhase*1.25)*(3*params.microFlutter)*profile.verticalFunnelEnvelope*profile.cursorGustAmp;
          const depthWavePhase=profile.warpedX*.0072-profile.travelTime*1.25+v*Math.PI;
          const fineDepthZ=(Math.sin(depthWavePhase)*22+Math.cos(depthWavePhase*1.8+.5)*10)*params.fineDepthWave*profile.verticalFunnelEnvelope;
          const fineDepthY=Math.cos(depthWavePhase)*7.5*params.fineDepthWave*profile.verticalFunnelEnvelope;
          const dYLocal=sCurveY+flutterY+strandMicroY+fineDepthY;
          const dZLocal=dynamicCatenaryZ+flutterZ+strandMicroZ+fineDepthZ;
          const deltaY=s*sinR+dYLocal*cosR-dZLocal*sinR;
          const deltaZ=s*cosR+dYLocal*sinR+dZLocal*cosR;
          const worldX=colX+deltaZ*sinR*.06;
          const worldY=profile.spineY+deltaY;
          const worldZ=profile.spineZ+deltaZ;
          const normDepth=clamp((160-worldZ)/320,0,1);
          const strandSeed=((Math.sin(k*12.9898+row*78.233)*43758.5453)%1+1)%1;
          const depthGate=clamp((normDepth-.04)/(.34*params.backgroundSparsity+.001),0,1);
          const softStrandRatio=clamp((depthGate-(strandSeed-.22))/.28,0,1);
          const strandDissolve=softStrandRatio*softStrandRatio*(3-2*softStrandRatio);
          const effectiveDissolve=edgeDissolve*(.15+.85*strandDissolve);
          if(effectiveDissolve<=.003)continue;
          const scale=focalLength/(focalLength+worldZ);
          const projectedX=centerX+(worldX-centerX)*scale;
          const projectedY=centerY+(worldY-centerY)*scale;
          if(projectedX< -8||projectedX>width+8||projectedY< -8||projectedY>height+8)continue;
          const normElevation=clamp((centerY+75-worldY)/150,0,1);
          const prominence=normDepth*.76+normElevation*.24;
          const foldBoost=Math.max(0,(foldHighlight-.90)*.42);
          const sizeMultiplier=.65+Math.pow(prominence,2.6)*(params.depthContrast*.52)+foldBoost*.25+facingFactor*.26+profile.centerConvergence*.30;
          const dissolveScale=Math.pow(effectiveDissolve,1.35);
          const radius=params.dotSize*sizeMultiplier*(.74+.26*profile.outerTaperFactor)*dissolveScale;
          if(radius<.12)continue;
          const sheen=Math.abs(deltaY)/(halfW+1);
          const colorMetric=(prominence*.68+foldBoost*.20+sheen*.12+profile.centerConvergence*.10+pointerGust*.08)*Math.pow(effectiveDissolve,.75);
          const tierIndex=Math.floor(clamp(colorMetric*10,0,9.99));
          const bucket=clothBuckets[tierIndex];
          if(bucket.count>=maxDotsPerTier)continue;
          const offset=bucket.count*3;
          bucket.coords[offset]=projectedX;bucket.coords[offset+1]=projectedY;bucket.coords[offset+2]=radius;
          bucket.count++;
        }
      }
    }
    canvas.dataset.tierCounts=clothBuckets.map(bucket=>String(bucket.count)).join(',');
    canvas.dataset.maxPointerGust=String(maxPointerGust);
    return clothBuckets;
  };
  const drawLogin=seconds=>{
    ctx.clearRect(0,0,width,height);
    const card=document.querySelector('.login-card');
    if(!card)return;
    const rect=card.getBoundingClientRect();
    const mobile=width<600;
    centerX=width*.5;
    centerY=rect.top+rect.height*.5;
    params=mobile?{...desktopBaseline,meshRows:13,meshSpacingX:15,sheetWidth:96,centerMinWidth:24,dotSize:.62,depthContrast:2.7,silhouetteAmp:.62,heightEnvelope:.72,macroTwist:.58,rollTorsion:.56,fineDepthWave:.55,edgeFlutter:.48,drapeCamber:.48,cursorGust:.13,cursorRadius:120}:desktopBaseline;
    canvas.dataset.profile=mobile?'restrained-mobile':'gold-standard-desktop';
    const buckets=collectSurfaceVertices(seconds);
    const pi2=Math.PI*2;
    if(mobile)ctx.globalAlpha=.82;
    for(let tierIndex=0;tierIndex<buckets.length;tierIndex++){
      const bucket=buckets[tierIndex];
      if(!bucket.count)continue;
      ctx.fillStyle=tierColors[tierIndex];
      ctx.beginPath();
      for(let i=0;i<bucket.count*3;i+=3){
        const x=bucket.coords[i],y=bucket.coords[i+1],radius=bucket.coords[i+2];
        ctx.moveTo(x+radius,y);ctx.arc(x,y,radius,0,pi2);
      }
      ctx.fill();
    }
    ctx.globalAlpha=1;
  };
  const drawDots=()=>{
    ctx.clearRect(0,0,width,height);
    const spacing=34;
    for(let y=18;y<height;y+=spacing){
      for(let x=18;x<width;x+=spacing){
        const dx=pointer.x-x,dy=pointer.y-y;
        const local=Math.exp(-(dx*dx+dy*dy)/(2*150*150))*pointer.amount;
        const alpha=.04+local*.034;
        const radius=.92+local*.18;
        ctx.fillStyle=rgba(218,126,82,alpha);
        ctx.beginPath();ctx.arc(x,y,radius,0,Math.PI*2);ctx.fill();
      }
    }
  };
  const draw=seconds=>{if(mode==='login-fabric')drawLogin(seconds);else drawDots(seconds)};
  const tick=now=>{
    frame=0;
    if(document.hidden)return;
    if(now-last<33){frame=requestAnimationFrame(tick);return;}
    const elapsed=Math.min(.06,(now-last)/1000||0);
    last=now;time+=elapsed;
    if(mode==='login-fabric'){
      pointer.x+=(pointer.targetX-pointer.x)*.10;
      pointer.y+=(pointer.targetY-pointer.y)*.10;
      const recovery=params?params.cursorRecovery:desktopBaseline.cursorRecovery;
      pointer.amount+=(pointer.target-pointer.amount)*(1-Math.pow(1-recovery,elapsed*20));
    }else{
      pointer.amount+=(pointer.target-pointer.amount)*(1-Math.pow(.94,elapsed*60));
    }
    draw(time);
    const pointerSettled=mode==='app-dots'&&Math.abs(pointer.amount-pointer.target)<.002&&Math.abs(pointer.targetX-pointer.x)<.5&&Math.abs(pointer.targetY-pointer.y)<.5;
    if(!reduced.matches&&(mode==='login-fabric'||!pointerSettled))frame=requestAnimationFrame(tick);
  };
  const start=()=>{
    if(frame)cancelAnimationFrame(frame);
    frame=0;last=0;
    if(mode==='app-dots'&&reduced.matches){pointer.target=0;pointer.amount=0;}
    draw(time);
    if(!reduced.matches&&!document.hidden&&(mode==='login-fabric'||Math.abs(pointer.amount-pointer.target)>=.002))frame=requestAnimationFrame(tick);
  };
  pointer.targetX=-10000;pointer.targetY=-10000;
  window.addEventListener('pointermove',event=>{
    pointer.targetX=event.clientX;pointer.targetY=event.clientY;
    pointer.target=mode==='app-dots'&&reduced.matches?0:1;
    if(mode==='app-dots'){pointer.x=event.clientX;pointer.y=event.clientY;}
    if(mode==='app-dots'&&!reduced.matches&&!frame&&!document.hidden){last=performance.now();frame=requestAnimationFrame(tick);}
  },{passive:true});
  window.addEventListener('pointerleave',()=>{
    pointer.target=0;
    if(mode==='app-dots'&&!reduced.matches&&!frame&&!document.hidden){last=performance.now();frame=requestAnimationFrame(tick);}
  },{passive:true});
  document.addEventListener('visibilitychange',start);
  window.addEventListener('resize',resize,{passive:true});
  reduced.addEventListener?.('change',start);
  resize();start();
})();
  </script>`
