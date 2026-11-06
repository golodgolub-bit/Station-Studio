(()=>{'use strict';
// A TV switch is an explicit DPAD button, with the original input retained as
// the sole state/control source. No separate TV settings or double toggles.
const switches=new Map();
function decorate(){if(!document.body.classList.contains('station-tv'))return;for(const input of document.querySelectorAll('input[type=checkbox]')){if(switches.has(input))continue;const label=input.closest('label'),b=document.createElement('button');b.type='button';b.className='tv-switch';b.setAttribute('role','switch');b.setAttribute('aria-label',label?.querySelector('span')?.textContent||'Переключатель');input.classList.add('tv-switch-input');input.tabIndex=-1;input.setAttribute('aria-hidden','true');input.after(b);b.onclick=e=>{e.preventDefault();e.stopPropagation();if(!input.disabled){input.click();sync(input,b)}};switches.set(input,b);sync(input,b)}}
function sync(input,b){b.textContent=input.checked?'Включено':'Выключено';b.setAttribute('aria-checked',String(input.checked));b.disabled=input.disabled}
new MutationObserver(decorate).observe(document.body,{subtree:true,childList:true});decorate();setInterval(()=>{if(document.hidden)return;for(const [input,b]of switches){if(!input.isConnected){switches.delete(input);continue}sync(input,b)}},500);
})();
