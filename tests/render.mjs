import {chromium} from 'playwright-core';
import fs from 'node:fs';
const browser=await chromium.launch({channel:'chrome',headless:true});
let failed=false;
try{for(const [screen,width,height]of [['tv-visual',1280,720],['tv-ring',1920,1080],['tv-switch',960,540],['phone-home',390,844],['phone-ring',360,740],['phone-music',430,932],['phone-visual',390,844],['phone-small',320,568],['tablet-home',768,1024],['pc-home',1440,960],['phone-profile',390,844],['phone-variant',390,844],['phone-display',390,844],['tv-profile',1280,720],['tv-variant',1920,1080],['phone-guide',360,800],['phone-guide-small',320,568],['phone-guide-auth',390,844],['phone-guide-paired',390,844],['phone-guide-offline',320,568],['phone-guide-journal',390,844]]){
 const page=await browser.newPage({viewport:{width,height},deviceScaleFactor:1});const errors=[];page.on('pageerror',e=>errors.push(e.message));
 if(screen.includes('guide')){
  await page.addInitScript(({screen})=>{window.Setup={back(){},open(){},discover(){},cancel(){SetupUI.receive({type:'busy',value:false})},journal(){},authorize(){SetupUI.receive({type:'authorizationCommand',value:'PUBLIC KEY COMMAND · fixture'})},inspect(){SetupUI.receive({type:'busy',value:true});if(screen.endsWith('auth'))SetupUI.receive({type:'failure',value:{code:'ADB_AUTH_REQUIRED',port:5556}});else if(screen.endsWith('offline'))SetupUI.receive({type:'failure',value:{code:'NO_LOCAL_NETWORK',port:0}});else SetupUI.receive({type:'inspection',value:{host:'192.168.50.42',model:'yandexstation_2',build:'5000000921',compatible:!screen.endsWith('paired'),root:screen.endsWith('paired'),permissive:screen.endsWith('paired'),panel:screen.endsWith('paired'),paired:screen.endsWith('paired')}});SetupUI.receive({type:'busy',value:false})},importRoot(){SetupUI.receive({type:'archive',value:true})},root(){},rebootRoot(){},installPanel(){},connect(){}}},{screen});
  await page.goto('http://127.0.0.1:18890/guide/onboarding.html');await page.evaluate(()=>SetupUI.receive({type:'environment',value:{host:'192.168.50.42',android:36,bridgeInstalled:true,network:{wifi:true},phone:'Fixture'}}));await page.waitForTimeout(650);const checks=[];
  const check=(value,message)=>checks.push({pass:!!value,message});
  if(screen.endsWith('auth')){check(await page.locator('#recovery').isVisible(),'Отказ ключу объясняется отдельно от отсутствия ADB');check((await page.locator('[data-check=adb]').textContent()).includes('Включён'),'Работающий ADB не считается выключенным');await page.locator('#authorize').click();check(await page.locator('#authorizationBox').isVisible(),'Доступна передача публичного ключа');check(await page.locator('#install').isDisabled(),'Без авторизации ничего не устанавливается')}
  else if(screen.endsWith('paired')){check((await page.locator('#connect').textContent())==='Открыть мою станцию','Установленная панель открывается без нового кода');check(!(await page.locator('[data-step="4"]').textContent()).includes('введите этот код'),'Повторное сопряжение не предлагается');check(await page.locator('#install').isHidden(),'Повторная установка не навязывается')}
  else if(screen.endsWith('offline'))check((await page.locator('#recoveryTitle').textContent()).includes('Wi-Fi'),'Отсутствие локальной сети имеет конкретное действие');
  else if(screen.endsWith('journal')){await page.evaluate(()=>SetupUI.receive({type:'journal',value:[{time:Date.now(),code:'ADB_AUTH_REQUIRED',title:'Ключ не разрешён',detail:'Порт 5556'},{time:Date.now(),code:'ADB_CONNECT',title:'Проверяем ADB',detail:'Порт 5555'}]}));await page.locator('#journalBox summary').click();check(await page.locator('#journalEntries li').count()===2,'Журнал показывает отдельные этапы');await page.locator('#journalBox').scrollIntoViewIfNeeded();}
  else if(screen==='phone-guide'){check(await page.locator('[data-step="2"]').evaluate(e=>e.classList.contains('active')),'ADB ведёт к подходящему root-профилю');check(await page.locator('#runRoot').isDisabled(),'Без проверенного архива root недоступен')}
  check(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'Мастер не переполняет экран по горизонтали');
  check(await page.locator('#steps button').evaluateAll(nodes=>nodes.every(n=>n.getBoundingClientRect().height>=44)),'Кнопки шагов имеют доступную область нажатия');
  await page.screenshot({path:'artifacts/'+screen+'.png'});fs.writeFileSync('artifacts/'+screen+'.json',JSON.stringify({checks,pageErrors:errors},null,2));if(errors.length||checks.some(c=>!c.pass))failed=true;console.log(screen,JSON.stringify({checks,errors}));await page.close();continue;
 }

 await page.goto('http://127.0.0.1:18890/'+(screen.startsWith('tv')?'tv.html':'index.html')+'?screen='+screen);
 try{await page.waitForFunction(()=>document.documentElement.dataset.qaDone==='true',{timeout:20000})}catch(e){errors.push(e.message)}
 await page.screenshot({path:'artifacts/'+screen+'.png',fullPage:false});
 const result=await page.locator('#qaResult').textContent().then(JSON.parse).catch(()=>({checks:[],errors:['No report']}));result.pageErrors=errors;
 fs.writeFileSync('artifacts/'+screen+'.json',JSON.stringify(result,null,2));if(result.errors.length||errors.length||result.checks.some(c=>!c.pass))failed=true;
 console.log(screen,JSON.stringify(result));await page.close();
}}finally{await browser.close()}
if(failed)process.exit(1);
