package local.station.studio;

import java.util.*;

/** Small deterministic color quantizer. Keeps cover colors instead of averaging bands. */
public final class CoverPalette {
 public static int[] extract(int[] pixels) {
  int[] counts=new int[32768];long[] rs=new long[32768],gs=new long[32768],bs=new long[32768];
  for(int color:pixels){if((color>>>24)<128)continue;int r=color>>16&255,g=color>>8&255,b=color&255;int key=(r>>3)<<10|(g>>3)<<5|(b>>3);counts[key]++;rs[key]+=r;gs[key]+=g;bs[key]+=b;}
  ArrayList<Integer> bins=new ArrayList<>();int first=0;
  for(int i=0;i<counts.length;i++)if(counts[i]>0){bins.add(i);if(counts[i]>counts[first])first=i;}
  if(bins.isEmpty())return new int[]{0,0,0,0};
  int k=Math.min(4,bins.size());double[][] centers=new double[k][3];
  centers[0]=new double[]{rs[first]/(double)counts[first],gs[first]/(double)counts[first],bs[first]/(double)counts[first]};
  for(int c=1;c<k;c++){double best=-1;int chosen=first;for(int bin:bins){double r=rs[bin]/(double)counts[bin],g=gs[bin]/(double)counts[bin],b=bs[bin]/(double)counts[bin],nearest=Double.MAX_VALUE;for(int j=0;j<c;j++)nearest=Math.min(nearest,distance(r,g,b,centers[j]));double score=nearest*Math.sqrt(counts[bin]);if(score>best){best=score;chosen=bin;}}centers[c]=new double[]{rs[chosen]/(double)counts[chosen],gs[chosen]/(double)counts[chosen],bs[chosen]/(double)counts[chosen]};}
  long[] weights=new long[k];
  for(int pass=0;pass<8;pass++){double[][] sums=new double[k][3];Arrays.fill(weights,0);for(int bin:bins){double r=rs[bin]/(double)counts[bin],g=gs[bin]/(double)counts[bin],b=bs[bin]/(double)counts[bin],nearest=Double.MAX_VALUE;int target=0;for(int j=0;j<k;j++){double d=distance(r,g,b,centers[j]);if(d<nearest){nearest=d;target=j;}}weights[target]+=counts[bin];sums[target][0]+=rs[bin];sums[target][1]+=gs[bin];sums[target][2]+=bs[bin];}for(int j=0;j<k;j++)if(weights[j]>0)for(int c=0;c<3;c++)centers[j][c]=sums[j][c]/weights[j];}
  Integer[] order=new Integer[k];for(int i=0;i<k;i++)order[i]=i;Arrays.sort(order,(a,b)->Long.compare(weights[b],weights[a]));
  int[] result=new int[4];for(int i=0;i<4;i++){double[] c=centers[order[i%k]];result[i]=0xff000000|((int)Math.round(c[0])<<16)|((int)Math.round(c[1])<<8)|(int)Math.round(c[2]);}return result;
 }
 private static double distance(double r,double g,double b,double[] c){return 2*(r-c[0])*(r-c[0])+4*(g-c[1])*(g-c[1])+3*(b-c[2])*(b-c[2]);}
}
