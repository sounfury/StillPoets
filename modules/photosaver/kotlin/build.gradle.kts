// The Kotlin of the native module photosaver: an Android library, which mygo
// build adds to the app's Gradle build.
plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "stillpoets.modules.photosaver"
    defaultConfig {
        consumerProguardFiles("consumer-rules.pro")
    }
}

dependencies {
}
