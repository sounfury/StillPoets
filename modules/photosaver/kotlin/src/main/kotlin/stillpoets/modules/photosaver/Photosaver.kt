package stillpoets.modules.photosaver

import android.content.ContentValues
import android.media.MediaScannerConnection
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import dev.mygo.Native
import java.io.File
import java.io.FileInputStream
import java.io.IOException

fun makeSaver(): Saver = AndroidPhotoSaver()

private class AndroidPhotoSaver : Saver {
    override fun saveImage(
        filePath: String,
        albumName: String,
    ) {
        val ctx = Native.context
        val src = File(filePath)
        require(src.exists() && src.isFile && src.length() > 0L) {
            "卡片图片文件不存在或为空：$filePath"
        }

        val safeAlbum = albumName.trim().ifEmpty { "StillPoets" }.replace(Regex("""[/\\:*?"<>|]"""), "_")
        val ext = src.extension.lowercase()
        val mime =
            when (ext) {
                "jpg", "jpeg" -> "image/jpeg"
                "webp" -> "image/webp"
                else -> "image/png"
            }
        val displayName = src.name.ifBlank { "stillpoets_card_${System.currentTimeMillis()}.png" }

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            val resolver = ctx.contentResolver
            val values =
                ContentValues().apply {
                    put(MediaStore.Images.Media.DISPLAY_NAME, displayName)
                    put(MediaStore.Images.Media.MIME_TYPE, mime)
                    put(MediaStore.Images.Media.RELATIVE_PATH, "${Environment.DIRECTORY_PICTURES}/$safeAlbum")
                    put(MediaStore.Images.Media.IS_PENDING, 1)
                }
            val uri =
                resolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values)
                    ?: throw IOException("无法在系统相册中创建图片条目")
            try {
                resolver.openOutputStream(uri)?.use { out ->
                    FileInputStream(src).use { input ->
                        input.copyTo(out)
                    }
                } ?: throw IOException("无法打开系统相册写入流")

                val done =
                    ContentValues().apply {
                        put(MediaStore.Images.Media.IS_PENDING, 0)
                    }
                resolver.update(uri, done, null, null)
            } catch (e: Exception) {
                resolver.delete(uri, null, null)
                throw e
            }
            return
        }

        // Android 8.0 - 9（API 26-28）回退路径：写入公共 Pictures/<album> 并通知媒体库扫描
        @Suppress("DEPRECATION")
        val picturesDir = Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_PICTURES)
        val albumDir = File(picturesDir, safeAlbum)
        if (!albumDir.exists() && !albumDir.mkdirs()) {
            throw IOException("当前系统未开放相册目录写入权限，请改用「系统分享」保存")
        }
        val dst = File(albumDir, displayName)
        try {
            src.copyTo(dst, overwrite = true)
        } catch (e: SecurityException) {
            throw IOException("当前系统未授予存储权限，请改用「系统分享」保存", e)
        }
        MediaScannerConnection.scanFile(ctx, arrayOf(dst.absolutePath), arrayOf(mime), null)
    }
}
