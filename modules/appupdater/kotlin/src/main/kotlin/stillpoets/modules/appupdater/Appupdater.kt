package stillpoets.modules.appupdater

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.core.content.FileProvider
import dev.mygo.Native
import java.io.File

fun makeInstaller(): Installer = AndroidAppUpdater()

private class AndroidAppUpdater : Installer {
    override fun canInstallPackages(): Boolean {
        val ctx = Native.context
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            return true
        }
        return ctx.packageManager.canRequestPackageInstalls()
    }

    override fun openInstallPermissionSettings() {
        val ctx = Native.activity ?: Native.context
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) {
            return
        }
        val uri = Uri.parse("package:${ctx.packageName}")
        val intent =
            Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, uri).apply {
                if (Native.activity == null) {
                    addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                }
            }
        ctx.startActivity(intent)
    }

    override fun installAPK(apkPath: String) {
        val ctx = Native.context
        val src = File(apkPath)
        require(src.exists() && src.isFile && src.length() > 0L) {
            "APK 文件不存在或为空：$apkPath"
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && !ctx.packageManager.canRequestPackageInstalls()) {
            throw SecurityException("尚未开启「允许安装未知应用」权限")
        }

        val shareable = ensureInFileProviderRoot(ctx, src)
        val authority = "${ctx.packageName}.mygo.files"
        val contentUri = FileProvider.getUriForFile(ctx, authority, shareable)

        val intent =
            Intent(Intent.ACTION_VIEW).apply {
                setDataAndType(contentUri, "application/vnd.android.package-archive")
                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
        val launcher = Native.activity ?: ctx
        launcher.startActivity(intent)
    }

    override fun deviceABIs(): String {
        val abis = Build.SUPPORTED_ABIS ?: return ""
        return abis.joinToString(",")
    }

    private fun ensureInFileProviderRoot(
        ctx: Context,
        file: File,
    ): File {
        val canonical = file.canonicalPath
        val allowedRoots =
            listOfNotNull(
                ctx.filesDir?.canonicalPath,
                ctx.cacheDir?.canonicalPath,
                ctx.getExternalFilesDir(null)?.canonicalPath,
            )
        if (allowedRoots.any { root -> canonical == root || canonical.startsWith(root + File.separator) }) {
            return file
        }
        val updatesDir = File(ctx.cacheDir, "updates").apply { mkdirs() }
        val target = File(updatesDir, file.name)
        file.copyTo(target, overwrite = true)
        return target
    }
}
