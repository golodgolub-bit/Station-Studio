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
  while(props.find())add(props.group(1).endsWith("gateway")?gateways:dns,props.group(3));
  return new JSONObject().put("router",gateways.size()==1?gateways.iterator().next():"").put("dns",dns.size()==1?dns.iterator().next():"").put("routers",new JSONArray(gateways)).put("resolvers",new JSONArray(dns));
 }
 private static void add(Set<String> values,String value){try{String valid=StationDiscovery.local(value);if(!valid.equals(value))return;values.add(valid);}catch(Exception ignored){}}
}
