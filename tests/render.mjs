import {chromium} from 'playwright-core';
import fs from 'node:fs';
const browser=await chromium.launch({channel:'chrome',headless:true});
let failed=false;
try{for(const [screen,width,height]of [['tv-visual',1280,720],['tv-ring',1920,1080],['tv-switch',960,540],['phone-home',390,844],['phone-ring',360,740],['phone-music',430,932],['phone-visual',390,844],['phone-small',320,568],['tablet-home',768,1024],['pc-home',1440,960]]){
 const page=await browser.newPage({viewport:{width,height},deviceScaleFactor:1});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto('http://127.0.0.1:18890/'+(screen.startsWith('tv')?'tv.html':'index.html')+'?screen='+screen);
 try{await page.waitForFunction(()=>document.documentElement.dataset.qaDone==='true',{timeout:20000})}catch(e){errors.push(e.message)}
 await page.screenshot({path:'artifacts/'+screen+'.png',fullPage:false});
 const result=await page.locator('#qaResult').textContent().then(JSON.parse).catch(()=>({checks:[],errors:['No report']}));result.pageErrors=errors;
 fs.writeFileSync('artifacts/'+screen+'.json',JSON.stringify(result,null,2));if(result.errors.length||errors.length||result.checks.some(c=>!c.pass))failed=true;
 console.log(screen,JSON.stringify(result));await page.close();
}}finally{await browser.close()}
if(failed)process.exit(1);
