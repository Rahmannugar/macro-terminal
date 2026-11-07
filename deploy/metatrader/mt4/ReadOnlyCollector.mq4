#property strict

input long ExpectedLogin = 0;
input string ExpectedServer = "";
input long HistoryFrom = 0;
input long HistoryTo = 0;
input string AttemptId = "";

const int MaximumRecords = 10000;
const uint MaximumReadMilliseconds = 30000;
const string OutputName = "macro-terminal-source.json";

string Quoted(string value)
{
   string result = "\"";
   for(int index = 0; index < StringLen(value); index++)
   {
      ushort character = StringGetCharacter(value, index);
      if(character == 34)
         result += "\\\"";
      else if(character == 92)
         result += "\\\\";
      else if(character < 32)
         result += StringFormat("\\u%04x", (int)character);
      else
         result += ShortToString(character);
   }
   return result + "\"";
}

string Number(double value)
{
   return Quoted(StringFormat("%.17g", value));
}

bool IdentityMatches()
{
   return IsConnected()
      && AccountInfoInteger(ACCOUNT_LOGIN) == ExpectedLogin
      && AccountServer() == ExpectedServer
      && !AccountInfoInteger(ACCOUNT_TRADE_ALLOWED);
}

bool WriteText(int handle, string value)
{
   ResetLastError();
   uint written = FileWriteString(handle, value);
   return written > 0 && GetLastError() == 0;
}

string AccountRecord()
{
   string record = "{\"login\":" + Quoted(StringFormat("%I64d", ExpectedLogin));
   record += ",\"server\":" + Quoted(AccountServer());
   record += ",\"currency\":" + Quoted(AccountCurrency());
   record += ",\"leverage\":" + Quoted(IntegerToString(AccountLeverage()));
   record += ",\"balance\":" + Number(AccountBalance());
   record += ",\"credit\":" + Number(AccountCredit());
   record += ",\"equity\":" + Number(AccountEquity());
   record += ",\"profit\":" + Number(AccountProfit());
   record += ",\"margin\":" + Number(AccountMargin());
   record += ",\"free_margin\":" + Number(AccountFreeMargin());
   return record + ",\"trade_allowed\":false}";
}

string OrderRecord()
{
   string record = "{\"ticket\":" + Quoted(IntegerToString(OrderTicket()));
   record += ",\"type\":" + Quoted(IntegerToString(OrderType()));
   record += ",\"symbol\":" + Quoted(OrderSymbol());
   record += ",\"volume\":" + Number(OrderLots());
   record += ",\"open_time_epoch\":" + Quoted(StringFormat("%I64d", (long)OrderOpenTime()));
   record += ",\"close_time_epoch\":" + Quoted(StringFormat("%I64d", (long)OrderCloseTime()));
   record += ",\"expiration_epoch\":" + Quoted(StringFormat("%I64d", (long)OrderExpiration()));
   record += ",\"open_price\":" + Number(OrderOpenPrice());
   record += ",\"close_price\":" + Number(OrderClosePrice());
   record += ",\"stop_loss\":" + Number(OrderStopLoss());
   record += ",\"take_profit\":" + Number(OrderTakeProfit());
   record += ",\"profit\":" + Number(OrderProfit());
   record += ",\"commission\":" + Number(OrderCommission());
   record += ",\"swap\":" + Number(OrderSwap());
   record += ",\"magic\":" + Quoted(IntegerToString(OrderMagicNumber()));
   return record + ",\"comment\":" + Quoted(OrderComment()) + "}";
}

bool WriteOrders(int handle, int pool, int count, uint started, int &records)
{
   records = 0;
   for(int index = 0; index < count; index++)
   {
      if(IsStopped() || GetTickCount() - started > MaximumReadMilliseconds || !IdentityMatches())
         return false;
      if(!OrderSelect(index, SELECT_BY_POS, pool))
         return false;
      if(pool == MODE_HISTORY && (OrderCloseTime() < HistoryFrom || OrderCloseTime() > HistoryTo))
         continue;
      if(records > 0 && !WriteText(handle, ","))
         return false;
      if(!WriteText(handle, OrderRecord()))
         return false;
      records++;
      if(FileSize(handle) > 16 * 1024 * 1024)
         return false;
   }
   return true;
}

void Failure(string code)
{
   FileDelete(OutputName + ".partial");
   Print("connector.mt4.source_read_failed: " + code);
}

void OnStart()
{
   FileDelete(OutputName);
   FileDelete(OutputName + ".partial");
   if(ExpectedLogin <= 0 || ExpectedServer == "" || HistoryFrom <= 0
       || HistoryTo <= HistoryFrom || HistoryTo - HistoryFrom > 7 * 24 * 60 * 60 || StringLen(AttemptId) != 32)
   {
      Failure("invalid_request");
      return;
   }
   uint waitStarted = GetTickCount();
   while(!IsConnected() && !IsStopped() && GetTickCount() - waitStarted < 60000)
      Sleep(250);
   if(!IsConnected())
   {
      Failure("broker_disconnected");
      return;
   }
   if(!IdentityMatches())
   {
      Failure("identity_or_read_only_mismatch");
      return;
   }
   int openCount = OrdersTotal();
   int historyCount = OrdersHistoryTotal();
   if(openCount > MaximumRecords || historyCount > MaximumRecords)
   {
      Failure("record_limit_exceeded");
      return;
   }
   int handle = FileOpen(OutputName + ".partial", FILE_WRITE | FILE_TXT | FILE_ANSI, 0, CP_UTF8);
   if(handle == INVALID_HANDLE)
   {
      Failure("output_unavailable");
      return;
   }
   uint started = GetTickCount();
   string header = "{\"schema_version\":1,\"platform\":\"mt4\",\"terminal_build\":";
    header += Quoted(IntegerToString((int)TerminalInfoInteger(TERMINAL_BUILD)));
    header += ",\"attempt_id\":" + Quoted(AttemptId);
   header += ",\"observed_at_epoch\":" + Quoted(StringFormat("%I64d", (long)TimeGMT()));
   header += ",\"account\":" + AccountRecord();
   header += ",\"read_only_verified\":false";
   header += ",\"read_only_evidence\":\"connected_identity_and_trade_disabled_flag_only\"";
   header += ",\"source_numbers\":\"decimal_strings_from_platform_binary64_and_integer_values\"";
   header += ",\"history_coverage\":{\"status\":\"terminal_loaded_only\",\"full_account_history_verified\":false";
   header += ",\"loaded_history_count\":" + IntegerToString(historyCount);
   header += ",\"from_epoch\":" + Quoted(StringFormat("%I64d", (long)HistoryFrom));
   header += ",\"to_epoch\":" + Quoted(StringFormat("%I64d", (long)HistoryTo)) + "}";
   header += ",\"source_time_basis\":\"broker_terminal_time_utc_offset_unverified\"";
   header += ",\"snapshot_consistency\":\"sequential_reads_with_identity_and_count_rechecks_not_transactional\"";
   header += ",\"open_orders\":[";
   int openRecords = 0;
   int historyRecords = 0;
   bool complete = WriteText(handle, header)
      && WriteOrders(handle, MODE_TRADES, openCount, started, openRecords)
      && WriteText(handle, "],\"history_orders\":[")
      && WriteOrders(handle, MODE_HISTORY, historyCount, started, historyRecords)
      && WriteText(handle, "]}")
      && IdentityMatches() && OrdersTotal() == openCount && OrdersHistoryTotal() == historyCount;
   FileFlush(handle);
   FileClose(handle);
   if(!complete)
   {
      Failure("source_read_incomplete");
      return;
   }
   if(!FileMove(OutputName + ".partial", 0, OutputName, FILE_REWRITE))
   {
      Failure("output_commit_failed");
      return;
   }
   Print("connector.mt4.source_read_completed: open_records=" + IntegerToString(openRecords)
      + " history_records=" + IntegerToString(historyRecords));
}
