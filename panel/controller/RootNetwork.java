package local.station.controller;

import org.json.*;
import java.util.*;
import java.util.regex.*;

/** Read-only network evidence from the station, never the phone's VPN/default route. */
final class RootNetwork {
 static final String COMMAND="ip -4 route show table all 2>/dev/null; echo ALICEKIT_DHCP; getprop | grep -E '^\\[(dhcp\\.[a-zA-Z0-9_]+\\.(gateway|dns[12])|net\\.dns[12])\\]' || true";
 static JSONObject parse(String output)throws Exception {
  Set<String> gateways=new LinkedHashSet<>(),dns=new LinkedHashSet<>();
  Matcher routes=Pattern.compile("(?m)^default via ([0-9.]+)(?: .*|$)").matcher(output);
  while(routes.find())add(gateways,routes.group(1));
  Matcher props=Pattern.compile("(?m)^\\[(dhcp\\.[a-zA-Z0-9_]+\\.(gateway|dns[12])|net\\.dns[12])\\]: \\[([0-9.]+)\\]$").matcher(output);
  while(props.find()){if(props.group(1).endsWith("gateway"))add(gateways,props.group(3));else try{dns.add(dnsAddress(props.group(3)));}catch(Exception ignored){}}
  return new JSONObject().put("router",gateways.size()==1?gateways.iterator().next():"").put("dns",dns.size()==1?dns.iterator().next():"").put("routers",new JSONArray(gateways)).put("resolvers",new JSONArray(dns)).put("routerState",gateways.size()==1?"detected":gateways.isEmpty()?"missing":"ambiguous").put("dnsState",dns.size()==1?"detected":dns.isEmpty()?"missing":"ambiguous");
 }
 static String dnsAddress(String value)throws Exception{if(value==null||!value.matches("(?:0|[1-9][0-9]{0,2})(?:\\.(?:0|[1-9][0-9]{0,2})){3}"))throw new Exception("Укажите полный IPv4-адрес DNS");String[] p=value.split("\\.");for(String v:p)if(Integer.parseInt(v)>255)throw new Exception("В адресе DNS число больше 255");int first=Integer.parseInt(p[0]);if(first==0||first==127||first>=224)throw new Exception("Нужен обычный адрес DNS, не loopback или multicast");return value;}
 private static void add(Set<String> values,String value){try{String valid=StationDiscovery.local(value);if(!valid.equals(value))return;values.add(valid);}catch(Exception ignored){}}
}
