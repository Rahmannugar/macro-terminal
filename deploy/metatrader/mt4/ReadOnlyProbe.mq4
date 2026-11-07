#property strict

void OnStart()
{
   int handle = FileOpen("macro-terminal-probe.json", FILE_WRITE | FILE_TXT | FILE_ANSI);
   if(handle == INVALID_HANDLE)
   {
      Print("connector.mt4.probe_failed: output_unavailable");
      return;
   }

   bool connected = IsConnected();
   bool tradeAllowed = (bool)AccountInfoInteger(ACCOUNT_TRADE_ALLOWED);
   string result = "{\"platform\":\"mt4\",\"connected\":";
   result += connected ? "true" : "false";
   result += ",\"account_trade_allowed\":";
   result += tradeAllowed ? "true" : "false";
   result += ",\"open_order_count\":" + IntegerToString(OrdersTotal());
   result += ",\"loaded_history_count\":" + IntegerToString(OrdersHistoryTotal());
   result += ",\"read_only_verified\":false,\"history_coverage\":\"terminal_loaded_only\"}";
   FileWriteString(handle, result);
   FileFlush(handle);
   FileClose(handle);
   Print("connector.mt4.probe_completed: read_only_verification_pending");
}
