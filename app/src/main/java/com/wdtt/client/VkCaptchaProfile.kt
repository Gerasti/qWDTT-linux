package com.wdtt.client

import android.content.Context
import android.provider.Settings
import android.util.DisplayMetrics
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.security.SecureRandom

/**
 * Пишет vk_profile.json в filesDir для Go RJS-капчи:
 * стабильный browser_fp и device_json с реального экрана Android.
 */
object VkCaptchaProfile {

    private const val PROFILE_FILE = "vk_profile.json"
    private const val FP_FILE = "captcha_browser_fp"

    fun writeForGo(context: Context) {
        val appContext = context.applicationContext
        val androidId = Settings.Secure.getString(
            appContext.contentResolver,
            Settings.Secure.ANDROID_ID
        ) ?: "unknown"

        val ua = UserAgentGenerator.generateForDevice(androidId)
        val chromeMajor = Regex("Chrome/(\\d+)").find(ua)?.groupValues?.getOrNull(1) ?: "131"
        val secChUa =
            "\"Chromium\";v=\"$chromeMajor\", \"Not_A Brand\";v=\"24\", \"Google Chrome\";v=\"$chromeMajor\""

        val metrics = appContext.resources.displayMetrics
        val width = metrics.widthPixels.coerceAtLeast(320)
        val height = metrics.heightPixels.coerceAtLeast(480)
        val innerHeight = (height * 0.88).toInt().coerceAtLeast(400)

        val deviceJson = JSONObject().apply {
            put("screenWidth", width)
            put("screenHeight", height)
            put("screenAvailWidth", width)
            put("screenAvailHeight", height)
            put("innerWidth", width)
            put("innerHeight", innerHeight)
            put("devicePixelRatio", metrics.density.toDouble())
            put("language", "ru-RU")
            put("languages", JSONArray(listOf("ru-RU", "ru", "en-US")))
            put("webdriver", false)
            put(
                "hardwareConcurrency",
                Runtime.getRuntime().availableProcessors().coerceIn(4, 8)
            )
            put("notificationsPermission", "default")
        }

        val profile = JSONObject().apply {
            put("user_agent", ua)
            put("sec_ch_ua", secChUa)
            put("sec_ch_ua_mobile", "?1")
            put("sec_ch_ua_platform", "\"Android\"")
            put("device_json", deviceJson.toString())
            put("browser_fp", stableBrowserFp(appContext))
        }

        File(appContext.filesDir, PROFILE_FILE).writeText(profile.toString())
    }

    private fun stableBrowserFp(context: Context): String {
        val fpFile = File(context.filesDir, FP_FILE)
        if (fpFile.exists()) {
            fpFile.readText().trim().takeIf { it.length == 32 }?.let { return it }
        }
        val bytes = ByteArray(16)
        SecureRandom().nextBytes(bytes)
        val fp = bytes.joinToString("") { "%02x".format(it) }
        fpFile.writeText(fp)
        return fp
    }
}
