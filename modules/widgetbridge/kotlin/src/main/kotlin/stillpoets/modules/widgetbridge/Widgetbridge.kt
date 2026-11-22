package stillpoets.modules.widgetbridge

import android.appwidget.AppWidgetManager
import android.content.ComponentName
import android.os.Build
import dev.mygo.Native

fun makeBridge(): Bridge = AndroidWidgetBridge()

private class AndroidWidgetBridge : Bridge {
    override fun sync(timelinePath: String, fontsDir: String) {
        val ctx = Native.context
        StillPoetsWidgetProvider.savePathsAndRefresh(ctx, timelinePath, fontsDir)
    }

    override fun requestPin(): Boolean {
        val ctx = Native.activity ?: Native.context
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            return false
        }
        val manager = AppWidgetManager.getInstance(ctx) ?: return false
        if (!manager.isRequestPinAppWidgetSupported) {
            return false
        }
        val provider = ComponentName(ctx, StillPoetsWidgetProvider::class.java)
        return manager.requestPinAppWidget(provider, null, null)
    }

    override fun activeWidgetCount(): Long {
        val ctx = Native.context
        val manager = AppWidgetManager.getInstance(ctx) ?: return 0L
        val ids = manager.getAppWidgetIds(ComponentName(ctx, StillPoetsWidgetProvider::class.java))
        return ids.size.toLong()
    }
}
