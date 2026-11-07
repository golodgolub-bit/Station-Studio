import {chromium} from 'playwright-core';
import fs from 'node:fs';
const browser=await chromium.launch({channel:'chrome',headless:true});
let failed=false;
try{for(const [screen,width,height]of [['tv-visual',1280,720],['tv-ring',1920,1080],['tv-switch',960,540],['phone-home',390,844],['phone-ring',360,740],['phone-music',430,932],['phone-visual',390,844],['phone-small',320,568],['tablet-home',768,1024],['pc-home',1440,960],['phone-profile',390,844],['phone-variant',390,844],['phone-display',390,844],['tv-profile',1280,720],['tv-variant',1920,1080],['phone-guide',360,800],['phone-guide-small',320,568]]){
 const page=await browser.newPage({viewport:{width,height},deviceScaleFactor:1});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 if(screen.includes('guide')){await page.addInitScript(()=>{window.Setup={back(){},open(){},inspect(){SetupUI.receive({type:'inspection',value:{host:'192.168.50.42',model:'yandexstation_2',build:'5000000921',compatible:true,root:false,permissive:false,panel:false}})},importRoot(){SetupUI.receive({type:'archive',value:true})},root(){},rebootRoot(){},installPanel(){},connect(){}}});await page.goto('http://127.0.0.1:18890/guide/onboarding.html');await page.evaluate(()=>SetupUI.receive({type:'environment',value:{host:'192.168.50.42',android:35,bridgeInstalled:true}}));const checks=[];if(screen==='phone-guide'){await page.locator('#check').click();await page.waitForTimeout(400);checks.push({pass:await page.locator('[data-step="2"]').evaluate(e=>e.classList.contains('active')),message:'ADB ведёт к подходящему root-профилю'});checks.push({pass:await page.locator('#runRoot').isDisabled(),message:'Без проверенного архива root недоступен'});}else checks.push({pass:await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),message:'Мастер помещается на узком телефоне'});await page.screenshot({path:'artifacts/'+screen+'.png'});fs.writeFileSync('artifacts/'+screen+'.json',JSON.stringify({checks,pageErrors:errors},null,2));if(errors.length||checks.some(c=>!c.pass))failed=true;console.log(screen,JSON.stringify({checks,errors}));await page.close();continue;}
 await page.goto('http://127.0.0.1:18890/'+(screen.startsWith('tv')?'tv.html':'index.html')+'?screen='+screen);
 try{await page.waitForFunction(()=>document.documentElement.dataset.qaDone==='true',{timeout:20000})}catch(e){errors.push(e.message)}
 await page.screenshot({path:'artifacts/'+screen+'.png',fullPage:false});
 const result=await page.locator('#qaResult').textContent().then(JSON.parse).catch(()=>({checks:[],errors:['No report']}));result.pageErrors=errors;
 fs.writeFileSync('artifacts/'+screen+'.json',JSON.stringify(result,null,2));if(result.errors.length||errors.length||result.checks.some(c=>!c.pass))failed=true;
 console.log(screen,JSON.stringify(result));await page.close();
}}finally{await browser.close()}
if(failed)process.exit(1);
