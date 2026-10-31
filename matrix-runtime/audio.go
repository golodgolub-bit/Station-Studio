package main

// Read-only observer of the existing AudioRecord shared buffer. Android 9
// AudioTrackShared.h/Tracks.cpp define the checked cblk layout (Apache-2.0).
// No AudioRecord/ALSA device is opened; no consumer cursor is advanced.
import("encoding/binary";"fmt";"math";"os";"strconv";"strings";"time")
type audioTap struct{file *os.File;address int64;buffer int64;last uint32;progress time.Time;levels [7]float64;wave [512]float64;bands [16]float64}
func openAudioTap(pid string)(*audioTap,error){
 b,_:=os.ReadFile("/proc/"+pid+"/maps");var start,end uint64
 for _,line:=range strings.Split(string(b),"\n"){if strings.Contains(line,"/dev/ashmem/AudioFlinger::Client("+pid+")")&&strings.Contains(line,"rw-s"){if start!=0{return nil,fmt.Errorf("Неоднозначный буфер микрофонов")};p:=strings.Split(strings.Fields(line)[0],"-");start,_=strconv.ParseUint(p[0],16,64);end,_=strconv.ParseUint(p[1],16,64)}}
 if start==0||end-start>4<<20{return nil,fmt.Errorf("Буфер микрофонов не найден")};f,e:=os.Open("/proc/"+pid+"/mem");if e!=nil{return nil,e}
 fail:=func()(*audioTap,error){f.Close();return nil,fmt.Errorf("Формат звукового буфера не подтверждён")}
 a:=make([]byte,end-start);z:=make([]byte,end-start);if n,_:=f.ReadAt(a,int64(start));n!=len(a){return fail()};time.Sleep(160*time.Millisecond);if n,_:=f.ReadAt(z,int64(start));n!=len(z){return fail()}
 found:=-1
 for i:=0;i+0xd0+16384*32<=len(a);i+=16{
  u:=func(b []byte,o int)uint32{return binary.LittleEndian.Uint32(b[i+o:])}
  delta:=u(z,0)-u(a,0)
  if u(z,0xa8)!=9216||u(z,0xc)!=3072||u(z,0x10)!=0xe000e000||u(z,0x14)!=0||delta<3072||delta>24576{continue}
  rear,front:=u(z,0xb4),u(z,0xb0);if rear-front>9216||rear-u(z,0)>3072||u(z,0xb8)!=0||u(z,0xc8)!=0||u(z,0xcc)!=0{continue}
  if found>=0{return fail()};found=i
 }
 if found<0{return fail()};return &audioTap{file:f,address:int64(start)+int64(found),buffer:int64(start)+int64(found)+0xd0,progress:time.Now()},nil
}
func(a *audioTap)close(){if a!=nil&&a.file!=nil{a.file.Close();a.file=nil};if a!=nil{a.wave=[512]float64{};a.bands=[16]float64{};a.levels=[7]float64{}}}
func(a *audioTap)sample()error{
 h:=make([]byte,0xd0);if n,_:=a.file.ReadAt(h,a.address);n!=len(h)||binary.LittleEndian.Uint32(h[0xa8:])!=9216||binary.LittleEndian.Uint32(h[0xc:])!=3072{return fmt.Errorf("Звуковой буфер изменился")}
 rear:=binary.LittleEndian.Uint32(h[0xb4:]);if rear!=a.last{a.progress=time.Now();a.last=rear};if time.Since(a.progress)>2*time.Second{return fmt.Errorf("Микрофонный поток остановлен")}
 // Read 1536 completed frames, decimate to 16 kHz for visual analysis only.
 const count=1536;data:=make([]byte,count*32);position:=(rear-count)&16383;first:=count;if int(position)+count>16384{first=16384-int(position)}
 if n,_:=a.file.ReadAt(data[:first*32],a.buffer+int64(position)*32);n!=first*32{return fmt.Errorf("Нет доступа к микрофонному буферу")};if first<count{if n,_:=a.file.ReadAt(data[first*32:],a.buffer);n!=(count-first)*32{return fmt.Errorf("Нет доступа к звуку")}}
 sums:=[7]float64{};for i:=0;i<count;i+=3{for ch:=0;ch<7;ch++{v:=float64(int16(binary.LittleEndian.Uint16(data[i*32+ch*2:])))/32768;sums[ch]+=v*v}}
 best:=0;for ch:=0;ch<7;ch++{a.levels[ch]=math.Sqrt(sums[ch]/512);if sums[ch]>sums[best]{best=ch}}
 mean:=0.0;for i:=0;i<512;i++{a.wave[i]=float64(int16(binary.LittleEndian.Uint16(data[i*3*32+best*2:])))/32768;mean+=a.wave[i]};mean/=512
 frequencies:=[]float64{80,120,180,270,400,600,850,1200,1700,2300,3000,3800,4600,5400,6300,7200}
 for band,freq:=range frequencies{coefficient:=2*math.Cos(2*math.Pi*freq/16000);s1,s2:=0.0,0.0;for i,v:=range a.wave{v=(v-mean)*(.5-.5*math.Cos(2*math.Pi*float64(i)/511));s:=v+coefficient*s1-s2;s2=s1;s1=s};power:=math.Sqrt(math.Max(0,s1*s1+s2*s2-coefficient*s1*s2))/128;a.bands[band]=power}
 // Discard PCM after deriving local levels and a short in-memory waveform.
 clear(data);return nil
}
func reactiveFrame(a *audioTap,mode string,gain float64,t float64)[]byte{
 out:=make([]byte,400);put:=func(x,y int,v float64){if x>=0&&x<25&&y>=0&&y<16{out[y*25+x]=byte(math.Max(0,math.Min(255,v)))}}
 level:=0.0;for _,v:=range a.levels{if v>level{level=v}};level=math.Min(1,level*gain*10)
 switch mode{
 case "spectrum":for x:=0;x<25;x++{b:=x*16/25;height:=math.Min(16,math.Max(0,(20*math.Log10(math.Max(1e-7,a.bands[b]*gain))+65)/4));for y:=0;y<int(height);y++{put(x,15-y,100+float64(y)*10)}}
 case "wave":for x:=0;x<25;x++{v:=0.0;for k:=0;k<20;k++{sample:=a.wave[x*20+k];if math.Abs(sample)>math.Abs(v){v=sample}};y:=8-int(math.Max(-7,math.Min(7,v*gain*25)));put(x,y,255);put(x,y+1,80)}
 case "pulse":radius:=1+level*10;for y:=0;y<16;y++{for x:=0;x<25;x++{d:=math.Hypot(float64(x-12),float64(y)-7.5);put(x,y,255*math.Max(0,1-math.Abs(d-radius)/2))}}
 case "particles":for k:=0;k<30;k++{speed:=.2+level*2;x:=12+math.Sin(float64(k)*2.4+t*speed)*(2+level*11);y:=7.5+math.Cos(float64(k)*1.7+t*speed)*(1+level*7);put(int(x),int(y),70+level*185)}
 };return out
}
