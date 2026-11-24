# 保留 MyGo 原生桥接入口，避免 release R8 裁剪或重命名
-keep class stillpoets.modules.appupdater.MygoModule { public static ** INSTANCE; }
