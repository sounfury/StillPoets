import Foundation
#if canImport(Photos)
import Photos
#endif

// StillPoets iOS 原生相册写入实现：
// 仅申请最小权限 .addOnly（不读取用户已有照片），通过 PHAssetCreationRequest 直接无损写入原始 PNG 文件。

func makeSaver() throws -> any Saver { ApplePhotoSaver() }

private final class _PhotoResultBox: @unchecked Sendable {
    var error: Error?
}

private enum PhotoSaverError: LocalizedError {
    case fileNotFound(String)
    case permissionDenied
    case saveFailed(String)

    var errorDescription: String? {
        switch self {
        case .fileNotFound(let p):
            return "卡片图片文件不存在：\(p)"
        case .permissionDenied:
            return "未获得相册写入权限，请在系统设置中开启或改用「系统分享」"
        case .saveFailed(let msg):
            return "保存到相册失败：\(msg)"
        }
    }
}

private final class ApplePhotoSaver: Saver {
    func saveImage(filePath: String, albumName: String) throws {
        #if canImport(Photos)
        guard !filePath.isEmpty, FileManager.default.fileExists(atPath: filePath) else {
            throw PhotoSaverError.fileNotFound(filePath)
        }
        let fileURL = URL(fileURLWithPath: filePath)

        // 1. 检查或请求仅添加照片权限（iOS 14+ .addOnly）
        let status: PHAuthorizationStatus
        if #available(iOS 14, *) {
            let current = PHPhotoLibrary.authorizationStatus(for: .addOnly)
            if current == .notDetermined {
                let sem = DispatchSemaphore(value: 0)
                var granted: PHAuthorizationStatus = .denied
                PHPhotoLibrary.requestAuthorization(for: .addOnly) { next in
                    granted = next
                    sem.signal()
                }
                _ = sem.wait(timeout: .now() + 60)
                status = granted
            } else {
                status = current
            }
        } else {
            status = PHPhotoLibrary.authorizationStatus()
        }

        guard status == .authorized || status == .limited else {
            throw PhotoSaverError.permissionDenied
        }

        // 2. 以无损原始文件形式写入系统相册
        let sem = DispatchSemaphore(value: 0)
        let box = _PhotoResultBox()
        PHPhotoLibrary.shared().performChanges({
            let req = PHAssetCreationRequest.forAsset()
            let opts = PHAssetResourceCreationOptions()
            opts.shouldMoveFile = false
            opts.originalFilename = fileURL.lastPathComponent
            req.addResource(with: .photo, fileURL: fileURL, options: opts)
        }, completionHandler: { success, err in
            if let err = err {
                box.error = PhotoSaverError.saveFailed(err.localizedDescription)
            } else if !success {
                box.error = PhotoSaverError.saveFailed("系统未确认写入完成")
            }
            sem.signal()
        })
        _ = sem.wait(timeout: .now() + 30)
        if let err = box.error {
            throw err
        }
        #else
        throw NativeError.unsupported
        #endif
    }
}
