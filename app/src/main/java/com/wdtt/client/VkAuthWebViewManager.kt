package com.wdtt.client

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.util.Base64
import android.util.Log
import android.view.ViewGroup
import android.webkit.CookieManager
import android.webkit.JavascriptInterface
import android.webkit.SslErrorHandler
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.net.http.SslError
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.app.NotificationCompat
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeout
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
import kotlin.coroutines.cancellation.CancellationException

data class VkTurnCreds(
    val username: String,
    val password: String,
    val urls: List<String>
)

enum class VkAuthScreenMode {
    LOGIN,
    JOIN_CALL,
}

object VkAuthWebViewManager {
    private const val TAG = "VkAuthWV"
    private const val AUTH_TIMEOUT_MS = 5 * 60_000L
    private const val NOTIFICATION_ID = 9002
    private const val CHANNEL_ID = "vk_auth_channel"

    const val EXTRA_MODE = "authMode"
    const val EXTRA_JOIN_HASH = "joinHash"

    val authMutex = Mutex()
    private val pendingLoginResult = AtomicReference<CompletableDeferred<Result<Unit>>?>(null)
    private val pendingTurnResult = AtomicReference<CompletableDeferred<Result<VkTurnCreds>>?>(null)
    var activeActivity: VkAuthActivity? = null
    var pendingIntentToStart: Intent? = null

    /** Вход в аккаунт VK без присоединения к звонку (кнопка на главной). */
    suspend fun loginOnly(context: Context): Result<Unit> {
        return authMutex.withLock {
            val deferred = CompletableDeferred<Result<Unit>>()
            pendingLoginResult.getAndSet(deferred)?.cancel()

            val intent = Intent(context, VkAuthActivity::class.java).apply {
                putExtra(EXTRA_MODE, VkAuthScreenMode.LOGIN.name)
                addFlags(
                    Intent.FLAG_ACTIVITY_NEW_TASK or
                        Intent.FLAG_ACTIVITY_SINGLE_TOP or
                        Intent.FLAG_ACTIVITY_CLEAR_TOP or
                        Intent.FLAG_ACTIVITY_EXCLUDE_FROM_RECENTS
                )
            }
            pendingIntentToStart = intent
            showAuthNotification(context, VkAuthScreenMode.LOGIN, null)

            if (MainActivity.isForeground) {
                context.startActivity(intent)
            }

            try {
                withTimeout(AUTH_TIMEOUT_MS) {
                    deferred.await()
                }
            } finally {
                pendingLoginResult.set(null)
                pendingIntentToStart = null
                clearAuthNotification(context)
                try {
                    activeActivity?.finish()
                } catch (_: Exception) {
                }
                activeActivity = null
            }
        }
    }

    /** Присоединение к звонку и получение TURN (при подключении туннеля). */
    suspend fun authenticate(context: Context, hash: String): Result<VkTurnCreds> {
        val cleanHash = stripVkUrlStatic(hash.trim())
        if (cleanHash.length < 8) {
            return Result.failure(IllegalArgumentException("Некорректный VK-хеш"))
        }
        return authMutex.withLock {
            val deferred = CompletableDeferred<Result<VkTurnCreds>>()
            pendingTurnResult.getAndSet(deferred)?.cancel()

            val intent = Intent(context, VkAuthActivity::class.java).apply {
                putExtra(EXTRA_MODE, VkAuthScreenMode.JOIN_CALL.name)
                putExtra(EXTRA_JOIN_HASH, cleanHash)
                addFlags(
                    Intent.FLAG_ACTIVITY_NEW_TASK or
                        Intent.FLAG_ACTIVITY_SINGLE_TOP or
                        Intent.FLAG_ACTIVITY_CLEAR_TOP or
                        Intent.FLAG_ACTIVITY_EXCLUDE_FROM_RECENTS
                )
            }
            pendingIntentToStart = intent
            showAuthNotification(context, VkAuthScreenMode.JOIN_CALL, cleanHash)

            if (MainActivity.isForeground) {
                context.startActivity(intent)
            }

            try {
                withTimeout(AUTH_TIMEOUT_MS) {
                    deferred.await()
                }
            } finally {
                pendingTurnResult.set(null)
                pendingIntentToStart = null
                clearAuthNotification(context)
                try {
                    activeActivity?.finish()
                } catch (_: Exception) {
                }
                activeActivity = null
            }
        }
    }

    suspend fun authenticateAll(context: Context, hashes: List<String>): Map<String, VkTurnCreds> {
        val out = linkedMapOf<String, VkTurnCreds>()
        for (hash in hashes.distinct()) {
            val clean = stripVkUrlStatic(hash.trim())
            if (clean.length < 8) continue
            val result = authenticate(context, clean)
            if (result.isFailure) {
                throw result.exceptionOrNull() ?: Exception("VK auth failed for $clean")
            }
            out[clean] = result.getOrThrow()
        }
        return out
    }

    fun writeCredsFile(context: Context, credsByHash: Map<String, VkTurnCreds>): File {
        val root = JSONObject()
        val hashes = JSONObject()
        credsByHash.forEach { (hash, creds) ->
            val entry = JSONObject()
            entry.put("u", creds.username)
            entry.put("p", creds.password)
            entry.put("urls", JSONArray(creds.urls))
            hashes.put(hash, entry)
        }
        root.put("hashes", hashes)
        val file = File(context.filesDir, "vk_turn_creds.json")
        file.writeText(root.toString())
        return file
    }

    fun notifyLoginFailure(error: Exception) {
        val deferred = pendingLoginResult.getAndSet(null) ?: return
        if (!deferred.isCompleted) {
            deferred.complete(Result.failure(error))
        }
    }

    fun notifyLoginSuccess() {
        val deferred = pendingLoginResult.getAndSet(null) ?: return
        if (!deferred.isCompleted) {
            deferred.complete(Result.success(Unit))
        }
    }

    fun notifyTurnResult(result: Result<VkTurnCreds>) {
        val deferred = pendingTurnResult.getAndSet(null) ?: return
        if (!deferred.isCompleted) {
            deferred.complete(result)
        }
    }

    fun notifyCancelled() {
        pendingLoginResult.getAndSet(null)?.complete(Result.failure(CancellationException("Cancelled")))
        pendingTurnResult.getAndSet(null)?.complete(Result.failure(CancellationException("Cancelled")))
    }

    fun checkAndShowPendingAuth(context: Context) {
        val intent = pendingIntentToStart
        if (intent != null && activeActivity == null) {
            context.startActivity(intent)
        }
    }

    fun hasVkSessionCookie(): Boolean = vkRemixSid().isNotBlank()

    internal fun vkRemixSid(): String {
        val cm = CookieManager.getInstance()
        val raw = listOf(
            cm.getCookie("https://vk.com"),
            cm.getCookie("https://vk.ru"),
            cm.getCookie("https://m.vk.com"),
            cm.getCookie("https://m.vk.ru"),
        ).filterNotNull().joinToString(";")
        return raw.split(";")
            .map { it.trim() }
            .firstOrNull { it.startsWith("remixsid=") }
            ?.removePrefix("remixsid=")
            ?.trim()
            .orEmpty()
    }

    private fun showAuthNotification(context: Context, mode: VkAuthScreenMode, hash: String?) {
        if (MainActivity.isForeground) return
        val notificationManager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                "Авторизация VK",
                NotificationManager.IMPORTANCE_HIGH
            )
            notificationManager.createNotificationChannel(channel)
        }
        val openIntent = Intent(context, VkAuthActivity::class.java).apply {
            putExtra(EXTRA_MODE, mode.name)
            if (hash != null) putExtra(EXTRA_JOIN_HASH, hash)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        }
        val openPendingIntent = PendingIntent.getActivity(
            context, 2, openIntent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
        val cancelIntent = Intent(context, VkAuthCancelReceiver::class.java)
        val cancelPendingIntent = PendingIntent.getBroadcast(
            context, 3, cancelIntent, PendingIntent.FLAG_IMMUTABLE
        )
        val body = when (mode) {
            VkAuthScreenMode.LOGIN -> "Войдите в аккаунт VK"
            VkAuthScreenMode.JOIN_CALL -> "Подтверждаем вход в звонок…"
        }
        val notification = NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.ic_dialog_info)
            .setContentTitle("Требуется вход в VK")
            .setContentText(body)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setContentIntent(openPendingIntent)
            .setAutoCancel(true)
            .addAction(0, "Отменить", cancelPendingIntent)
            .build()
        notificationManager.notify(NOTIFICATION_ID, notification)
    }

    private fun clearAuthNotification(context: Context) {
        val notificationManager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        notificationManager.cancel(NOTIFICATION_ID)
    }

    fun encodeTurnCredsPayload(hash: String, creds: VkTurnCreds): String {
        val json = JSONObject()
        json.put("u", creds.username)
        json.put("p", creds.password)
        json.put("urls", JSONArray(creds.urls))
        val b64 = Base64.encodeToString(json.toString().toByteArray(Charsets.UTF_8), Base64.NO_WRAP)
        return "TURN_CREDS|$hash|$b64\n"
    }

    internal fun logAuth(message: String, isError: Boolean = false) {
        Log.d(TAG, message)
        TunnelManager.addVkAuthLog(message, isError)
    }
}

class VkAuthActivity : ComponentActivity() {
    private lateinit var screenMode: VkAuthScreenMode
    private var loginHandled = false
    private var joinHash = ""
    private var joinUrlIndex = 0
    private var awaitingLoginBeforeJoin = false
    private var join404Retries = 0
    private var webViewRef: WebView? = null
    private val autoJoinScope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    private var autoJoinJob: Job? = null
    private var autoJoinRunId = 0

    private fun joinUrlCandidates(): List<String> = listOf(
        "https://m.vk.com/call/join/$joinHash",
        "https://vk.com/call/join/$joinHash",
        "https://vk.ru/call/join/$joinHash",
    )

    private fun currentJoinUrl(): String = joinUrlCandidates().getOrElse(joinUrlIndex) {
        joinUrlCandidates().last()
    }

    private val interceptorJSCode = """
        (function() {
            if (window.__wdtt_vk_auth_installed) return;
            window.__wdtt_vk_auth_installed = true;

            function tryEmitTurnServer(data) {
                if (!data) return;
                var ts = data.turn_server;
                if (!ts && data.response && data.response.turn_server) {
                    ts = data.response.turn_server;
                }
                if (ts && ts.username && ts.credential && ts.urls) {
                    window.WdttVkAuth.onTurnServer(JSON.stringify(ts));
                }
            }

            const origFetch = window.fetch;
            window.fetch = async function() {
                const response = await origFetch.apply(this, arguments);
                try {
                    const clone = response.clone();
                    const text = await clone.text();
                    if (text && text.indexOf('turn_server') !== -1) {
                        tryEmitTurnServer(JSON.parse(text));
                    }
                } catch(e) {}
                return response;
            };

            const origXHROpen = XMLHttpRequest.prototype.open;
            const origXHRSend = XMLHttpRequest.prototype.send;
            XMLHttpRequest.prototype.open = function(method, url) {
                this._wdtt_url = url;
                return origXHROpen.apply(this, arguments);
            };
            XMLHttpRequest.prototype.send = function() {
                const xhr = this;
                xhr.addEventListener('load', function() {
                    try {
                        if (!xhr.responseText || xhr.responseText.indexOf('turn_server') === -1) return;
                        tryEmitTurnServer(JSON.parse(xhr.responseText));
                    } catch(e) {}
                });
                return origXHRSend.apply(this, arguments);
            };
        })();
    """.trimIndent()

    private val autoJoinDiagJSCode = """
        (function() {
            function norm(s) {
                return (s || '').replace(/\s+/g, ' ').trim();
            }
            var buttons = [];
            var selectors = 'button, a, [role="button"], input[type="button"], input[type="submit"], .vkuiButton, [class*="Button"]';
            var nodes = document.querySelectorAll(selectors);
            for (var i = 0; i < nodes.length && buttons.length < 20; i++) {
                var el = nodes[i];
                var text = norm(el.innerText || el.textContent || el.value || el.getAttribute('aria-label'));
                if (!text) continue;
                buttons.push(text.substring(0, 100));
            }
            var body = '';
            try { body = norm((document.body && document.body.innerText) || '').substring(0, 400); } catch(e) {}
            return JSON.stringify({
                url: String(location.href || ''),
                ready: String(document.readyState || ''),
                title: String(document.title || ''),
                clicks: window.__wdtt_auto_join_clicks || 0,
                buttons: buttons,
                body: body
            });
        })();
    """.trimIndent()

    private val autoJoinSetupJSCode = """
        (function() {
            if (window.__wdtt_auto_join_ready) return;
            window.__wdtt_auto_join_ready = true;
            window.__wdtt_auto_join_clicks = 0;
            window.__wdtt_auto_join_done = false;

            var rejectPhrases = [
                'открыть в приложении',
                'open in app',
                'open in the',
                'vk звонки',
                'скачать приложение'
            ];

            function norm(s) {
                return (s || '').replace(/\s+/g, ' ').trim().toLowerCase();
            }

            function elementText(el) {
                return norm(el.innerText || el.textContent || el.value || el.getAttribute('aria-label') || '');
            }

            function scoreElement(el) {
                var text = elementText(el);
                if (!text || text.length > 120) return -1;
                for (var r = 0; r < rejectPhrases.length; r++) {
                    if (text.indexOf(rejectPhrases[r]) !== -1) return -1;
                }
                if (text.indexOf('продолжить в браузере') !== -1 && text.indexOf('открыть') !== -1 && text.length > 30) {
                    return -1;
                }
                if (text === 'продолжить в браузере' || text === 'continue in browser') return 100;
                if (text.indexOf('продолжить в браузере') !== -1) return 90 - Math.min(text.length, 80);
                if (text.indexOf('continue in browser') !== -1) return 85 - Math.min(text.length, 80);
                if (text.indexOf('присоединиться к звонку через браузер') !== -1) return 70;
                if (text.indexOf('присоединиться через браузер') !== -1) return 65;
                if (text.indexOf('войти в звонок') !== -1) return 50;
                if (text === 'продолжить' || text === 'continue') return 40;
                if (text.indexOf('продолжить') !== -1 && text.length <= 25) return 35;
                return -1;
            }

            function hasBetterChild(el, parentScore) {
                var kids = el.querySelectorAll('button, a, [role="button"], input[type="button"], input[type="submit"]');
                for (var i = 0; i < kids.length; i++) {
                    if (kids[i] === el) continue;
                    var cs = scoreElement(kids[i]);
                    if (cs >= parentScore) return true;
                }
                return false;
            }

            function pickBest(minScore) {
                var selectors = 'button, a, [role="button"], input[type="button"], input[type="submit"], .vkuiButton, [class*="Button"]';
                var nodes = document.querySelectorAll(selectors);
                var best = null;
                var bestScore = -1;
                var bestLen = 9999;
                for (var i = 0; i < nodes.length; i++) {
                    var el = nodes[i];
                    var sc = scoreElement(el);
                    if (sc < minScore) continue;
                    if (hasBetterChild(el, sc)) continue;
                    var tlen = elementText(el).length;
                    if (sc > bestScore || (sc === bestScore && tlen < bestLen)) {
                        best = el;
                        bestScore = sc;
                        bestLen = tlen;
                    }
                }
                return best ? { el: best, score: bestScore, text: elementText(best) } : null;
            }

            function fireClick(el) {
                try { el.click(); } catch (e1) {}
                try {
                    el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, view: window }));
                } catch (e2) {}
            }

            window.__wdtt_autoJoinTry = function() {
                if (window.__wdtt_auto_join_done) return '';
                var pick = pickBest(50) || pickBest(35);
                if (!pick) return '';
                fireClick(pick.el);
                window.__wdtt_auto_join_clicks = (window.__wdtt_auto_join_clicks || 0) + 1;
                window.__wdtt_auto_join_done = true;
                return 'clicked(score=' + pick.score + '):' + pick.text.substring(0, 60);
            };

            if (!window.__wdtt_auto_join_observer) {
                window.__wdtt_auto_join_observer = new MutationObserver(function() {
                    window.__wdtt_autoJoinTry();
                });
                var root = document.documentElement || document.body;
                if (root) {
                    window.__wdtt_auto_join_observer.observe(root, { childList: true, subtree: true });
                }
                setTimeout(function() {
                    try { window.__wdtt_auto_join_observer.disconnect(); } catch(e) {}
                }, 45000);
            }
        })();
    """.trimIndent()

    private val autoJoinTryJSCode =
        "(function(){ return (window.__wdtt_autoJoinTry && window.__wdtt_autoJoinTry()) || ''; })();"

    private fun resetAutoJoinForNewPage(view: WebView?) {
        autoJoinJob?.cancel()
        autoJoinRunId++
        view?.evaluateJavascript(
            "window.__wdtt_auto_join_clicks=0; window.__wdtt_auto_join_ready=false; window.__wdtt_auto_join_done=false;",
            null
        )
        VkAuthWebViewManager.logAuth("Страница загружается, сброс автоклика")
    }

    private fun parseJsJson(raw: String?): JSONObject? {
        if (raw.isNullOrBlank() || raw == "null") return null
        var s = raw.trim()
        if (s.startsWith("\"") && s.endsWith("\"")) {
            s = s.substring(1, s.length - 1)
                .replace("\\\"", "\"")
                .replace("\\\\", "\\")
        }
        return try {
            JSONObject(s)
        } catch (_: Exception) {
            null
        }
    }

    private fun dumpPageDiagnostics(view: WebView?, reason: String) {
        view?.evaluateJavascript(autoJoinDiagJSCode) { raw ->
            val obj = parseJsJson(raw)
            if (obj == null) {
                VkAuthWebViewManager.logAuth("Диагностика ($reason): не удалось прочитать страницу")
                return@evaluateJavascript
            }
            try {
                val url = obj.optString("url")
                val ready = obj.optString("ready")
                val title = obj.optString("title")
                val clicks = obj.optInt("clicks")
                val buttons = obj.optJSONArray("buttons")
                val body = obj.optString("body")
                val btnList = buildList {
                    if (buttons != null) {
                        for (i in 0 until buttons.length()) {
                            val t = buttons.optString(i)
                            if (t.isNotBlank()) add(t)
                        }
                    }
                }
                VkAuthWebViewManager.logAuth(
                    "Диагностика ($reason): ready=$ready, url=${url.take(80)}, title=${title.take(60)}, clicks=$clicks"
                )
                if (btnList.isEmpty()) {
                    VkAuthWebViewManager.logAuth("Кнопки на странице: не найдены")
                } else {
                    VkAuthWebViewManager.logAuth("Кнопки (${btnList.size}): ${btnList.take(8).joinToString(" | ")}")
                }
                if (body.isNotBlank()) {
                    VkAuthWebViewManager.logAuth("Текст страницы: ${body.take(200)}")
                }
            } catch (e: Exception) {
                VkAuthWebViewManager.logAuth("Диагностика ($reason): ошибка ${e.message}")
            }
        }
    }

    private fun scheduleAutoJoinClicks(view: WebView?) {
        if (screenMode != VkAuthScreenMode.JOIN_CALL || awaitingLoginBeforeJoin) return
        val wv = view ?: return
        val runId = ++autoJoinRunId
        autoJoinJob?.cancel()
        val clicked = AtomicBoolean(false)
        VkAuthWebViewManager.logAuth("Автоклик: старт (run=$runId, hash=${joinHash.take(8)}…)")
        wv.evaluateJavascript(autoJoinSetupJSCode, null)
        val delaysMs = longArrayOf(0, 500, 1000, 2000, 3000, 4000, 5000, 6500, 8000, 10000, 12000, 15000, 18000)
        val diagAtMs = setOf(3000L, 5000L, 8000L, 12000L, 18000L)
        autoJoinJob = autoJoinScope.launch {
            var prev = 0L
            for (target in delaysMs) {
                delay(target - prev)
                prev = target
                if (runId != autoJoinRunId) {
                    VkAuthWebViewManager.logAuth("Автоклик: отменён (новая страница)")
                    return@launch
                }
                wv.evaluateJavascript(autoJoinTryJSCode) { result ->
                    val clean = result?.trim()?.removeSurrounding("\"").orEmpty()
                    when {
                        clean.isNotBlank() && clean != "null" -> {
                            clicked.set(true)
                            VkAuthWebViewManager.logAuth("Автоклик @${target}ms: $clean")
                        }
                        diagAtMs.contains(target) && !clicked.get() -> dumpPageDiagnostics(wv, "${target}ms")
                    }
                }
            }
            delay(500)
            if (runId == autoJoinRunId && !clicked.get()) {
                VkAuthWebViewManager.logAuth(
                    "Автоклик: кнопка не найдена за 18с — нажмите «Продолжить» вручную",
                    isError = true
                )
            }
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        VkAuthWebViewManager.activeActivity = this

        screenMode = when (intent.getStringExtra(VkAuthWebViewManager.EXTRA_MODE)) {
            VkAuthScreenMode.LOGIN.name -> VkAuthScreenMode.LOGIN
            else -> VkAuthScreenMode.JOIN_CALL
        }
        val joinHashExtra = intent.getStringExtra(VkAuthWebViewManager.EXTRA_JOIN_HASH).orEmpty()
        joinHash = joinHashExtra
        if (screenMode == VkAuthScreenMode.JOIN_CALL && joinHash.length < 8) {
            VkAuthWebViewManager.notifyTurnResult(Result.failure(IllegalArgumentException("Некорректный VK-хеш")))
            finish()
            return
        }

        awaitingLoginBeforeJoin =
            screenMode == VkAuthScreenMode.JOIN_CALL && !VkAuthWebViewManager.hasVkSessionCookie()

        val startUrl = when (screenMode) {
            VkAuthScreenMode.LOGIN -> "https://m.vk.com/"
            VkAuthScreenMode.JOIN_CALL -> if (awaitingLoginBeforeJoin) "https://m.vk.com/" else currentJoinUrl()
        }
        val statusText = when (screenMode) {
            VkAuthScreenMode.LOGIN -> "Войдите в аккаунт VK"
            VkAuthScreenMode.JOIN_CALL -> when {
                awaitingLoginBeforeJoin -> "Сначала войдите в аккаунт VK"
                else -> "Подтверждаем вход в звонок…"
            }
        }

        CookieManager.getInstance().setAcceptCookie(true)
        VkAuthWebViewManager.logAuth(
            "WebView: mode=$screenMode, url=$startUrl, vkCookie=${VkAuthWebViewManager.hasVkSessionCookie()}, awaitingLogin=$awaitingLoginBeforeJoin"
        )

        setContent {
            MaterialTheme(colorScheme = if (isSystemInDarkTheme()) darkColorScheme() else lightColorScheme()) {
                var isLoading by rememberSaveable { mutableStateOf(true) }

                Box(modifier = Modifier.fillMaxSize()) {
                    Column(modifier = Modifier.fillMaxSize().statusBarsPadding()) {
                        Surface(
                            modifier = Modifier.fillMaxWidth(),
                            color = MaterialTheme.colorScheme.surfaceVariant
                        ) {
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                verticalAlignment = Alignment.CenterVertically
                            ) {
                                Text(
                                    text = statusText,
                                    modifier = Modifier
                                        .weight(1f)
                                        .padding(start = 16.dp, top = 10.dp, bottom = 10.dp, end = 8.dp),
                                    style = MaterialTheme.typography.bodyMedium
                                )
                                IconButton(
                                    onClick = {
                                        VkAuthWebViewManager.notifyCancelled()
                                        finish()
                                    }
                                ) {
                                    Icon(
                                        Icons.Default.Close,
                                        contentDescription = "Закрыть",
                                        tint = MaterialTheme.colorScheme.error
                                    )
                                }
                            }
                        }
                        Box(modifier = Modifier.weight(1f)) {
                            AndroidView(
                                modifier = Modifier.fillMaxSize(),
                                factory = { ctx ->
                                    WebView(ctx).apply {
                                        webViewRef = this
                                        CookieManager.getInstance().setAcceptThirdPartyCookies(this, true)
                                        setBackgroundColor(android.graphics.Color.WHITE)
                                        layoutParams = ViewGroup.LayoutParams(
                                            ViewGroup.LayoutParams.MATCH_PARENT,
                                            ViewGroup.LayoutParams.MATCH_PARENT
                                        )
                                        settings.apply {
                                            javaScriptEnabled = true
                                            domStorageEnabled = true
                                            databaseEnabled = true
                                            mediaPlaybackRequiresUserGesture = false
                                            loadWithOverviewMode = true
                                            useWideViewPort = true
                                            blockNetworkLoads = false
                                            cacheMode = WebSettings.LOAD_DEFAULT
                                            userAgentString =
                                                "Mozilla/5.0 (Linux; Android 13; Mobile) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
                                        }

                                        addJavascriptInterface(object {
                                            @JavascriptInterface
                                            fun onDebugLog(message: String) {
                                                VkAuthWebViewManager.logAuth("JS: $message")
                                            }

                                            @JavascriptInterface
                                            fun onTurnServer(json: String) {
                                                if (screenMode != VkAuthScreenMode.JOIN_CALL) return
                                                runOnUiThread {
                                                    parseAndFinishTurn(json)
                                                }
                                            }

                                            @JavascriptInterface
                                            fun onError(message: String) {
                                                when (screenMode) {
                                                    VkAuthScreenMode.LOGIN ->
                                                        VkAuthWebViewManager.notifyLoginFailure(Exception(message))
                                                    VkAuthScreenMode.JOIN_CALL ->
                                                        VkAuthWebViewManager.notifyTurnResult(
                                                            Result.failure(Exception(message))
                                                        )
                                                }
                                                finish()
                                            }
                                        }, "WdttVkAuth")

                                        webViewClient = object : WebViewClient() {
                                            override fun onPageStarted(view: WebView?, url: String?, favicon: android.graphics.Bitmap?) {
                                                super.onPageStarted(view, url, favicon)
                                                if (screenMode == VkAuthScreenMode.JOIN_CALL) {
                                                    resetAutoJoinForNewPage(view)
                                                    view?.evaluateJavascript(interceptorJSCode, null)
                                                }
                                            }

                                            override fun onPageFinished(view: WebView?, url: String?) {
                                                super.onPageFinished(view, url)
                                                if (screenMode == VkAuthScreenMode.JOIN_CALL) {
                                                    view?.evaluateJavascript(interceptorJSCode, null)
                                                    checkJoinPageOrRetry(view, url)
                                                    scheduleAutoJoinClicks(view)
                                                }
                                                if (screenMode == VkAuthScreenMode.LOGIN) {
                                                    checkLoginAndClose(url)
                                                }
                                                if (awaitingLoginBeforeJoin) {
                                                    checkLoginThenOpenJoin(view, url)
                                                }
                                            }

                                            override fun shouldOverrideUrlLoading(view: WebView?, request: WebResourceRequest?): Boolean {
                                                val url = request?.url?.toString().orEmpty()
                                                if (url.startsWith("intent://") || url.startsWith("vk://")) {
                                                    return true
                                                }
                                                return false
                                            }

                                            override fun onReceivedSslError(
                                                view: WebView?,
                                                handler: SslErrorHandler?,
                                                error: SslError?
                                            ) {
                                                val host = error?.url?.let { Uri.parse(it).host }.orEmpty()
                                                if (host.endsWith("vk.com") || host.endsWith("vk.ru") || host.endsWith("okcdn.ru")) {
                                                    handler?.proceed()
                                                } else {
                                                    handler?.cancel()
                                                }
                                            }
                                        }
                                        webChromeClient = object : WebChromeClient() {
                                            override fun onProgressChanged(view: WebView?, newProgress: Int) {
                                                if (newProgress >= 90) isLoading = false
                                            }
                                        }
                                        loadUrl(startUrl)
                                    }
                                }
                            )
                            if (isLoading) {
                                CircularProgressIndicator(
                                    modifier = Modifier.align(Alignment.Center).size(48.dp)
                                )
                            }
                        }
                    }
                }
            }
        }
    }

    private fun checkLoginThenOpenJoin(view: WebView?, pageUrl: String?) {
        if (!awaitingLoginBeforeJoin || screenMode != VkAuthScreenMode.JOIN_CALL) return
        val sid = VkAuthWebViewManager.vkRemixSid()
        if (sid.length < 8) return

        val url = pageUrl.orEmpty().lowercase()
        val onLoginFlow = url.contains("id.vk.com") && (url.contains("authorize") || url.contains("login"))
        if (onLoginFlow) return

        awaitingLoginBeforeJoin = false
        joinUrlIndex = 0
        join404Retries = 0
        VkAuthWebViewManager.logAuth("Вход VK OK, открываем звонок: ${currentJoinUrl()}")
        view?.loadUrl(currentJoinUrl())
    }

    private fun checkJoinPageOrRetry(view: WebView?, pageUrl: String?) {
        if (screenMode != VkAuthScreenMode.JOIN_CALL || awaitingLoginBeforeJoin) return
        view?.evaluateJavascript(
            "(function(){var t=document.body?document.body.innerText:'';return t.indexOf('Такой страницы нет')!==-1||t.indexOf('Страница не найдена')!==-1?'404':'';})();"
        ) { result ->
            if (result?.contains("404") != true) return@evaluateJavascript
            val candidates = joinUrlCandidates()
            if (joinUrlIndex < candidates.lastIndex) {
                joinUrlIndex++
                join404Retries++
                VkAuthWebViewManager.logAuth("404 на ${pageUrl ?: "?"}, retry ${joinUrlIndex + 1}/${candidates.size}")
                runOnUiThread { view.loadUrl(currentJoinUrl()) }
                return@evaluateJavascript
            }
            if (join404Retries == 0) return@evaluateJavascript
            Log.e("VkAuthWV", "VK call link not found for hash ${joinHash.take(8)}...")
            VkAuthWebViewManager.logAuth("Хеш звонка недействителен: ${joinHash.take(8)}…", isError = true)
            VkAuthWebViewManager.notifyTurnResult(
                Result.failure(
                    Exception("Ссылка на звонок VK недействительна или устарела. Обновите хеш звонка.")
                )
            )
            finish()
        }
    }

    private fun checkLoginAndClose(pageUrl: String?) {
        if (loginHandled || screenMode != VkAuthScreenMode.LOGIN) return
        val sid = VkAuthWebViewManager.vkRemixSid()
        if (sid.length < 8) return

        val url = pageUrl.orEmpty().lowercase()
        val onLoginFlow = url.contains("id.vk.com") && (url.contains("authorize") || url.contains("login"))
        if (onLoginFlow) return

        loginHandled = true
        VkAuthWebViewManager.logAuth("Вход VK выполнен, закрываем WebView")
        VkAuthWebViewManager.notifyLoginSuccess()
        finish()
    }

    private fun parseAndFinishTurn(json: String) {
        try {
            val obj = JSONObject(json)
            val user = obj.optString("username")
            val pass = obj.optString("credential")
            val urlsRaw = obj.optJSONArray("urls") ?: JSONArray()
            val urls = mutableListOf<String>()
            for (i in 0 until urlsRaw.length()) {
                val url = urlsRaw.optString(i)
                if (url.isNotBlank()) urls.add(url)
            }
            if (user.isBlank() || pass.isBlank() || urls.isEmpty()) {
                VkAuthWebViewManager.logAuth("Неполный turn_server: $json", isError = true)
                return
            }
            VkAuthWebViewManager.logAuth("TURN получены, urls=${urls.size}")
            VkAuthWebViewManager.notifyTurnResult(Result.success(VkTurnCreds(user, pass, urls)))
            finish()
        } catch (e: Exception) {
            Log.e("VkAuthWV", "parse error: ${e.message}")
        }
    }

    override fun onDestroy() {
        autoJoinJob?.cancel()
        super.onDestroy()
        if (VkAuthWebViewManager.activeActivity === this) {
            VkAuthWebViewManager.activeActivity = null
        }
    }
}

class VkAuthCancelReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        VkAuthWebViewManager.notifyCancelled()
        VkAuthWebViewManager.activeActivity?.finish()
        val notifMgr = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        notifMgr.cancel(9002)
    }
}
