# 保留桌面小组件 BroadcastReceiver 与 MyGo 原生桥接入口，避免 release R8 裁剪或重命名
-keep class stillpoets.modules.widgetbridge.StillPoetsWidgetProvider { *; }
-keep class stillpoets.modules.widgetbridge.MygoModule { public static ** INSTANCE; }
