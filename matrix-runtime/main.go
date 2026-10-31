// Station Max matrix service. Controller discovery follows YOpenSOS/maxpkg
// (Apache-2.0); this implementation rejects ambiguous/fallback offsets.
// A separate init service restores the original controller after a crash or TTL.
package main
import(
 "encoding/binary";"encoding/json";"fmt";"io";"net";"net/http";"os";"os/exec";"os/signal";"path/filepath";"strconv";"strings";"sync";"syscall";"time";"unsafe"
)
const folder="/data/local/station-matrix"
const socket="/data/local/tmp/station-panel/matrix.sock"
type controller struct{PID string `json:"pid"`;Pointer string `json:"pointer"`;Flag string `json:"flag"`;Value uint8 `json:"value"`;Relative int `json:"relative"`;Library string `json:"library"`;Start string `json:"start"`;Boot string `json:"boot"`;Frame []byte `json:"frame"`;Brightness string `json:"brightness"`}
type command struct{Action string `json:"action"`;Frames [][]byte `json:"frames"`;Delays []int `json:"delays"`;Duration int `json:"duration"`;Brightness int `json:"brightness"`;Reactive string `json:"reactive"`;Gain float64 `json:"gain"`}
var mu sync.Mutex
var current *controller
var memory,fb *os.File
var mapped []byte
var vinfo [160]byte
var frames [][]byte
var delays []int
var index int
var next,deadline time.Time
var lastError string
var lastSDKCheck time.Time
var tap *audioTap
var reactive string
var gain float64
var yielding bool
var displayBrightness int
var assistantChecked time.Time
var assistantSpeaking bool
func aliceBusy(now time.Time)bool{if now.Sub(assistantChecked)<100*time.Millisecond{return assistantSpeaking};assistantChecked=now;var state struct{State string `json:"state"`;Time int64 `json:"time"`;OverlayUntil int64 `json:"overlayUntil"`;AlarmActive bool `json:"alarmActive"`};json.Unmarshal([]byte(read("/data/local/tmp/station-panel/alice-state.json")),&state);assistantSpeaking=now.UnixMilli()<state.OverlayUntil || now.UnixMilli()-state.Time<20000&&(state.AlarmActive||state.State=="listening"||state.State=="thinking"||state.State=="speaking");return assistantSpeaking}
func handoff(now time.Time)bool{busy:=aliceBusy(now);if busy!=yielding&&memory!=nil&&current!=nil{if checkMemory(current,memory)!=nil{return false};value:=byte(0);brightness:=strconv.Itoa(displayBrightness);if busy{value=1;brightness=current.Brightness};if _,e:=memory.WriteAt([]byte{value},int64(number(current.Flag)));e!=nil{lastError=e.Error();return false};os.WriteFile("/sys/class/backlight/fb4/brightness",[]byte(brightness),0644);yielding=busy;next=time.Time{}};return yielding}

func read(path string)string{b,_:=os.ReadFile(path);return strings.TrimSpace(string(b))}
func starttime(pid string)string{stat:=read("/proc/"+pid+"/stat");i:=strings.LastIndex(stat,") ");if i<0{return ""};p:=strings.Fields(stat[i+2:]);if len(p)<20{return ""};return p[19]}
func number(s string)uint64{n,_:=strconv.ParseUint(strings.TrimPrefix(s,"0x"),16,64);return n}
func sdkValid(c *controller)bool{
 if c.Boot!=read("/proc/sys/kernel/random/boot_id")||c.Start==""||c.Start!=starttime(c.PID){return false};b,_:=os.ReadFile("/proc/"+c.PID+"/cmdline");return strings.SplitN(string(b),"\x00",2)[0]=="com.yandex.io.sdk"
}
func checkMemory(c *controller,f *os.File)error{
 if !sdkValid(c)||c.Relative!=-224||c.Library!="/system/priv-app/ServicesIosdk/ServicesIosdk.apk"{return fmt.Errorf("Контроллер дисплея изменился")}
 pointer,flag:=number(c.Pointer),number(c.Flag);if pointer<256||flag!=pointer-224{return fmt.Errorf("Неверный адрес контроллера")};b:=make([]byte,24);if n,_:=f.ReadAt(b,int64(pointer));n!=24||binary.LittleEndian.Uint32(b[16:20])!=16||binary.LittleEndian.Uint32(b[20:24])!=25{return fmt.Errorf("Размер матрицы не подтверждён")}
 mapping:=fmt.Sprintf("%x-",binary.LittleEndian.Uint64(b));maps:=read("/proc/"+c.PID+"/maps");found:=false;for _,line:=range strings.Split(maps,"\n"){if strings.HasPrefix(line,mapping)&&strings.HasSuffix(line,"/dev/graphics/fb4"){found=true}};if !found{return fmt.Errorf("Буфер дисплея не подтверждён")};return nil
}
func discover()(*controller,error){
 output,e:=exec.Command("/system/bin/sh","-c","exec /data/local/station-matrix/probe").Output();if e!=nil{return nil,e}
 var result struct{Unique bool `json:"unique"`;Candidates []controller `json:"candidates"`};if e=json.Unmarshal(output,&result);e!=nil{return nil,e};if !result.Unique||len(result.Candidates)!=1{return nil,fmt.Errorf("Не найден единственный проверенный контроллер дисплея")};c:=result.Candidates[0];c.Start=starttime(c.PID);c.Boot=read("/proc/sys/kernel/random/boot_id");return &c,nil
}
func recoverPrevious(){
 b,e:=os.ReadFile(folder+"/recovery.json");if e!=nil{return};var c controller;if json.Unmarshal(b,&c)!=nil{return}
 f,e:=os.OpenFile("/proc/"+c.PID+"/mem",os.O_RDWR,0);if e==nil{defer f.Close();if checkMemory(&c,f)==nil{value:=[]byte{0};if _,e=f.ReadAt(value,int64(number(c.Flag)));e==nil&&value[0]==0{f.WriteAt([]byte{1},int64(number(c.Flag)));if n,err:=strconv.Atoi(c.Brightness);err==nil&&n>=0&&n<=200{os.WriteFile("/sys/class/backlight/fb4/brightness",[]byte(c.Brightness),0644)}}}}
 os.Remove(folder+"/recovery.json")
}
func acquire()error{
 if current!=nil{if sdkValid(current){return nil};release()}
 c,e:=discover();if e!=nil{return e};f,e:=os.OpenFile("/proc/"+c.PID+"/mem",os.O_RDWR,0);if e!=nil{return e};if e=checkMemory(c,f);e!=nil{f.Close();return e}
 device,e:=os.OpenFile("/dev/graphics/fb4",os.O_RDWR,0);if e!=nil{f.Close();return e}
 _,_,err:=syscall.Syscall(syscall.SYS_IOCTL,device.Fd(),0x4600,uintptr(unsafe.Pointer(&vinfo[0])));if err!=0||binary.LittleEndian.Uint32(vinfo[0:4])!=25||binary.LittleEndian.Uint32(vinfo[4:8])!=16||binary.LittleEndian.Uint32(vinfo[24:28])!=24{device.Close();f.Close();return fmt.Errorf("Аппаратная матрица 25×16 RGB888 не подтверждена")}
 buffer,e:=syscall.Mmap(int(device.Fd()),0,4096,syscall.PROT_READ|syscall.PROT_WRITE,syscall.MAP_SHARED);if e!=nil{device.Close();f.Close();return e}
 c.Frame=append([]byte(nil),buffer[:1200]...);c.Brightness=read("/sys/class/backlight/fb4/brightness");data,_:=json.Marshal(c)
 recovery,e:=os.OpenFile(folder+"/recovery.tmp",os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0600);if e==nil{_,e=recovery.Write(data);if e==nil{e=recovery.Sync()};recovery.Close()};if e==nil{e=os.Rename(folder+"/recovery.tmp",folder+"/recovery.json")};if e!=nil{syscall.Munmap(buffer);device.Close();f.Close();return e}
 if _,e=f.WriteAt([]byte{0},int64(number(c.Flag)));e!=nil{syscall.Munmap(buffer);device.Close();f.Close();os.Remove(folder+"/recovery.json");return e}
 current=c;memory=f;fb=device;mapped=buffer;return nil
}
func release(){
 if tap!=nil{tap.close();tap=nil};reactive=""
 if current!=nil&&memory!=nil&&sdkValid(current){if checkMemory(current,memory)==nil{if !yielding&&len(mapped)>=1200&&len(current.Frame)==1200{copy(mapped[:1200],current.Frame);syscall.Syscall(syscall.SYS_IOCTL,fb.Fd(),0x4606,uintptr(unsafe.Pointer(&vinfo[0])))};memory.WriteAt([]byte{1},int64(number(current.Flag)));if n,e:=strconv.Atoi(current.Brightness);e==nil&&n>=0&&n<=200{os.WriteFile("/sys/class/backlight/fb4/brightness",[]byte(current.Brightness),0644)}}}
 if mapped!=nil{syscall.Munmap(mapped)};if fb!=nil{fb.Close()};if memory!=nil{memory.Close()};current=nil;yielding=false;mapped=nil;fb=nil;memory=nil;frames=nil;os.Remove(folder+"/recovery.json")
}
func reply(w http.ResponseWriter,status int,value any){w.Header().Set("Content-Type","application/json");w.WriteHeader(status);json.NewEncoder(w).Encode(value)}
func handle(w http.ResponseWriter,r *http.Request){
 mu.Lock();defer mu.Unlock()
 if r.Method=="GET"{reply(w,200,map[string]any{"ok":true,"active":current!=nil,"yieldingToAlice":yielding,"until":func()int64{if deadline.IsZero(){return 0};return deadline.Unix()}(),"error":lastError,"width":25,"height":16,"frames":len(frames),"reactive":reactive,"levels":func()any{if tap!=nil{return tap.levels};return nil}()});return}
 if r.Method!="POST"{reply(w,405,map[string]string{"error":"Нужен POST"});return};r.Body=http.MaxBytesReader(w,r.Body,128<<10);var q command;if json.NewDecoder(r.Body).Decode(&q)!=nil{reply(w,400,map[string]string{"error":"Некорректный запрос"});return}
 if q.Action=="stop"{release();reply(w,200,map[string]bool{"ok":true});return}
 if q.Reactive!=""{if q.Reactive!="spectrum"&&q.Reactive!="wave"&&q.Reactive!="pulse"&&q.Reactive!="particles"{reply(w,400,map[string]string{"error":"Неизвестная визуализация"});return};if q.Gain<0.25||q.Gain>20{reply(w,400,map[string]string{"error":"Чувствительность 0.25–20"});return};q.Frames=[][]byte{make([]byte,400)};q.Delays=[]int{100}}
 if q.Action!="play"||(q.Duration!=0&&q.Duration<5)||q.Duration>86400||q.Brightness<0||q.Brightness>200||len(q.Frames)==0||len(q.Frames)>120||len(q.Delays)!=len(q.Frames){reply(w,400,map[string]string{"error":"До 120 кадров, 5–86400 секунд, яркость 0–200"});return}
 for i,f:=range q.Frames{if len(f)!=400||q.Delays[i]<66||q.Delays[i]>10000{reply(w,400,map[string]string{"error":"Нужны кадры 25×16 и задержки 66–10000 мс"});return}}
 if read("/sys/fs/selinux/enforce")!="0"{reply(w,503,map[string]string{"error":"Ожидаем готовности root"});return};if e:=acquire();e!=nil{lastError=e.Error();reply(w,409,map[string]string{"error":lastError});return}
 if tap!=nil{tap.close();tap=nil};reactive=""
 if q.Reactive!=""{var e error;tap,e=openAudioTap(current.PID);if e!=nil{release();lastError=e.Error();reply(w,409,map[string]string{"error":lastError});return};reactive=q.Reactive;gain=q.Gain}
 displayBrightness=q.Brightness;frames=q.Frames;delays=q.Delays;index=0;next=time.Time{};deadline=time.Time{};if q.Duration!=0{deadline=time.Now().Add(time.Duration(q.Duration)*time.Second)};lastError="";os.WriteFile("/sys/class/backlight/fb4/brightness",[]byte(strconv.Itoa(q.Brightness)),0644);reply(w,200,map[string]any{"ok":true,"until":func()int64{if deadline.IsZero(){return 0};return deadline.Unix()}(),"frames":len(frames)})
}
func main(){
 os.MkdirAll(folder,0700);for i:=0;i<180&&read("/sys/fs/selinux/enforce")!="0";i++{time.Sleep(time.Second)};recoverPrevious();os.Remove(socket)
 listener,e:=net.Listen("unix",socket);if e!=nil{panic(e)};defer listener.Close();os.Chmod(socket,0600)
 signals:=make(chan os.Signal,1);signal.Notify(signals,syscall.SIGTERM,syscall.SIGINT);go func(){<-signals;mu.Lock();release();mu.Unlock();os.Exit(0)}()
 go func(){tick:=time.NewTicker(20*time.Millisecond);defer tick.Stop();for now:=range tick.C{mu.Lock();if current!=nil{check:=false;if now.Sub(lastSDKCheck)>time.Second{check=!sdkValid(current);lastSDKCheck=now};if (!deadline.IsZero()&&now.After(deadline))||check{release()}else if !handoff(now)&&!now.Before(next)&&len(frames)>0{f:=frames[index];if tap!=nil{if e:=tap.sample();e!=nil{lastError=e.Error();release();mu.Unlock();continue};f=reactiveFrame(tap,reactive,gain,float64(now.UnixMilli())/1000)};for i,v:=range f{mapped[i*3]=v;mapped[i*3+1]=v;mapped[i*3+2]=v};syscall.Syscall(syscall.SYS_IOCTL,fb.Fd(),0x4606,uintptr(unsafe.Pointer(&vinfo[0])));next=now.Add(time.Duration(delays[index])*time.Millisecond);index=(index+1)%len(frames)}};mu.Unlock()}}()
 server:=http.Server{Handler:http.HandlerFunc(handle),ReadHeaderTimeout:5*time.Second,ReadTimeout:20*time.Second,WriteTimeout:45*time.Second};e=server.Serve(listener);mu.Lock();release();mu.Unlock();if e!=nil&&e!=http.ErrServerClosed{fmt.Fprintln(os.Stderr,e)}
 _=io.EOF;_=filepath.Separator
}
