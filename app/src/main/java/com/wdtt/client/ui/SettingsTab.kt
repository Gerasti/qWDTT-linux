package com.wdtt.client.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Key
import androidx.compose.material.icons.filled.PowerSettingsNew
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material.icons.filled.Tag
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Public
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.wdtt.client.PeerAddress
import com.wdtt.client.SettingsStore
import com.wdtt.client.TunnelManager
import com.wdtt.client.TunnelService
import com.wdtt.client.WDTTColors
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.first
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material.icons.filled.CheckCircle
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import com.wdtt.client.VkAuthWebViewManager
import kotlin.math.roundToInt
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.net.Uri
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.material.icons.filled.Info
import androidx.compose.material3.Switch
import androidx.compose.material3.HorizontalDivider
import androidx.compose.ui.draw.scale
import com.wdtt.client.isNewerVersion
import com.wdtt.client.stripVkUrlStatic

private const val WORKERS_PER_GROUP = 9

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsTab(
    themeMode: String,
    onThemeChange: (String) -> Unit,
    isDynamicColor: Boolean,
    onDynamicColorChange: (Boolean) -> Unit,
    currentPalette: String,
    onPaletteChange: (String) -> Unit,
    onNavigateToLogs: () -> Unit = {}
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val settingsStore = remember { SettingsStore(context) }

    val currentDensity = LocalDensity.current
    CompositionLocalProvider(
        LocalDensity provides Density(currentDensity.density, fontScale = 1f)
    ) {
        SettingsTabContent(
            context = context,
            scope = scope,
            settingsStore = settingsStore,
            themeMode = themeMode,
            onThemeChange = onThemeChange,
            isDynamicColor = isDynamicColor,
            onDynamicColorChange = onDynamicColorChange,
            currentPalette = currentPalette,
            onPaletteChange = onPaletteChange,
            onNavigateToLogs = onNavigateToLogs
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsTabContent(
    context: android.content.Context,
    scope: kotlinx.coroutines.CoroutineScope,
    settingsStore: SettingsStore,
    themeMode: String,
    onThemeChange: (String) -> Unit,
    isDynamicColor: Boolean,
    onDynamicColorChange: (Boolean) -> Unit,
    currentPalette: String,
    onPaletteChange: (String) -> Unit,
    onNavigateToLogs: () -> Unit = {}
) {
    val savedConnectionPassword by settingsStore.connectionPassword.collectAsStateWithLifecycle(initialValue = "")
    val savedManualPortsEnabled by settingsStore.manualPortsEnabled.collectAsStateWithLifecycle(initialValue = false)
    val savedServerDtlsPort by settingsStore.serverDtlsPort.collectAsStateWithLifecycle(initialValue = 56000)
    val savedServerWgPort by settingsStore.serverWgPort.collectAsStateWithLifecycle(initialValue = 56001)
    val savedListenPort by settingsStore.listenPort.collectAsStateWithLifecycle(initialValue = 9000)

    val tunnelRunning by TunnelManager.running.collectAsStateWithLifecycle()
    val autoSwitchToLogs by settingsStore.autoSwitchToLogs.collectAsStateWithLifecycle(initialValue = true)
    val showSpeedGraph by settingsStore.showSpeedGraph.collectAsStateWithLifecycle(initialValue = true)
    val detailedLogs by settingsStore.detailedLogs.collectAsStateWithLifecycle(initialValue = false)
    val updateCheckIntervalHours by settingsStore.updateCheckIntervalHours.collectAsStateWithLifecycle(
        initialValue = com.wdtt.client.DEFAULT_UPDATE_CHECK_INTERVAL_HOURS
    )

    val currentProfileId by settingsStore.currentProfileId.collectAsStateWithLifecycle(initialValue = "")
    val currentProfileName by settingsStore.currentProfileName.collectAsStateWithLifecycle(initialValue = "")
    val savedPeer by settingsStore.peer.collectAsStateWithLifecycle(initialValue = "")
    val savedWorkers by settingsStore.workersPerHash.collectAsStateWithLifecycle(initialValue = 18)

    val profilesStore = remember { com.wdtt.client.ProfilesStore(context) }
    val profiles by profilesStore.profiles.collectAsStateWithLifecycle(initialValue = emptyList())

    val cooldownSeconds by TunnelManager.cooldownSeconds.collectAsStateWithLifecycle()
    var wasRunning by remember { mutableStateOf(false) }

    LaunchedEffect(tunnelRunning) {
        if (wasRunning && !tunnelRunning) {
            TunnelManager.startCooldown(5)
        }
        wasRunning = tunnelRunning
    }

    var peerInput by rememberSaveable { mutableStateOf("") }
    var vkHash1 by rememberSaveable { mutableStateOf("") }
    var vkHash2 by rememberSaveable { mutableStateOf("") }
    var vkHash3 by rememberSaveable { mutableStateOf("") }
    var vkHash4 by rememberSaveable { mutableStateOf("") }
    var workersInput by rememberSaveable { mutableFloatStateOf(18f) }
    var showHashesDialog by rememberSaveable { mutableStateOf(false) }
    var autoCaptchaEnabled by rememberSaveable { mutableStateOf(true) }
    var useWVCaptcha by rememberSaveable { mutableStateOf(false) }
    var isManualMode by rememberSaveable { mutableStateOf(true) }
    var wbvManualMode by rememberSaveable { mutableStateOf(true) }
    var vkAccountAuth by rememberSaveable { mutableStateOf(false) }
    var vkAuthBusy by remember { mutableStateOf(false) }
    var vkLoggedIn by remember { mutableStateOf(false) }
    var manualPortsEnabled by rememberSaveable { mutableStateOf(false) }
    var serverDtlsPortInput by rememberSaveable { mutableStateOf("56000") }
    var serverWgPortInput by rememberSaveable { mutableStateOf("56001") }
    var showAppSettingsDialog by rememberSaveable { mutableStateOf(false) }

    val currentHashesRaw by settingsStore.vkHashes.collectAsStateWithLifecycle(initialValue = "")
    val uniqueHashes = remember(currentHashesRaw) { 
        currentHashesRaw.split(Regex("[,\\s\\n]+"))
            .filter { it.isNotBlank() && it.length >= 16 }
            .distinct()
    }
    val filledHashCount = uniqueHashes.size
    val combinedHashes = uniqueHashes.joinToString(",")
    val dynamicMaxWorkers = remember(filledHashCount, vkAccountAuth) {
        if (vkAccountAuth) SettingsStore.VK_ACCOUNT_MAX_WORKERS.toFloat()
        else (filledHashCount.coerceAtLeast(1) * 27).toFloat()
    }
    
    val globalHashesRaw by settingsStore.globalVkHashes.collectAsStateWithLifecycle(initialValue = "")
    var portInput by rememberSaveable { mutableStateOf("9000") }
    var sniInput by rememberSaveable { mutableStateOf("") }



    val currentWorkers = if (vkAccountAuth) {
        workersInput.coerceIn(1f, dynamicMaxWorkers)
    } else {
        workersInput.coerceIn(WORKERS_PER_GROUP.toFloat(), dynamicMaxWorkers)
    }

    val hashErrors = remember(currentHashesRaw) {
        buildList {
            val parts = currentHashesRaw.split(Regex("[,\\s\\n]+")).filter { it.isNotEmpty() }
            parts.forEachIndexed { i, h ->
                if (h.isNotBlank() && h.length < 16) add("Хеш ${i + 1} — короткий")
            }
            val filled = parts.filter { it.isNotBlank() && it.length >= 16 }
            if (filled.size != filled.distinct().size) add("Есть дубликаты хешей")
        }
    }
    val hasInputHashErrors = hashErrors.isNotEmpty()

    var showSecretsDialog by rememberSaveable { mutableStateOf(false) }
    var initialized by remember { mutableStateOf(false) }

    fun normalizeHashes(vararg hashes: String): String {
        return hashes
            .map { stripVkUrlStatic(it) }
            .filter { it.isNotBlank() && it.length >= 16 }
            .distinct()
            .joinToString(",")
    }

    LaunchedEffect(Unit) {
        val peer = settingsStore.peer.first()
        val hashes = settingsStore.vkHashes.first()
        val workers = settingsStore.workersPerHash.first()
        val port = settingsStore.listenPort.first()
        val manualPorts = settingsStore.manualPortsEnabled.first()
        val serverDtlsPort = settingsStore.serverDtlsPort.first()
        val serverWgPort = settingsStore.serverWgPort.first()
        val sni = settingsStore.sni.first()
        val captchaMode = settingsStore.captchaMode.first()
        val captchaMethod = settingsStore.captchaSolveMethod.first()
        val wbvCaptchaMethod = settingsStore.captchaWbvSolveMethod.first()
        val vkAuthMode = settingsStore.vkAuthMode.first()
        
        peerInput = peer
        val initialHashesList = hashes.split(Regex("[,\\s\\n]+"))
            .filter { it.isNotBlank() && it.length >= 16 }
            .distinct()
        val initialHashesCount = initialHashesList.size.coerceAtLeast(1)
        workersInput = roundToGroup(workers.toFloat(), (initialHashesCount * 27).toFloat())
        portInput = port.toString()
        manualPortsEnabled = manualPorts
        serverDtlsPortInput = serverDtlsPort.toString()
        serverWgPortInput = serverWgPort.toString()
        sniInput = sni
        autoCaptchaEnabled = captchaMode == "auto"
        useWVCaptcha = captchaMode != "rjs"
        wbvManualMode = wbvCaptchaMethod != "auto"
        isManualMode = if (captchaMode == "wv") wbvManualMode else captchaMethod != "auto"
        vkAccountAuth = !vkAuthMode.equals("anonymous", ignoreCase = true)
        
        initialized = true
        vkLoggedIn = VkAuthWebViewManager.hasVkSessionCookie()
    }

    LaunchedEffect(currentProfileId, savedPeer, savedWorkers, savedListenPort) {
        if (currentProfileId.isBlank()) return@LaunchedEffect
        if (savedPeer.isNotBlank()) peerInput = savedPeer
        portInput = savedListenPort.toString()
        val hashesCount = combinedHashes.split(",").filter { it.isNotBlank() }.size.coerceAtLeast(1)
        val maxW = if (vkAccountAuth) SettingsStore.VK_ACCOUNT_MAX_WORKERS.toFloat() else (hashesCount * 27).toFloat()
        workersInput = roundToGroup(savedWorkers.toFloat(), maxW)
    }

    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                vkLoggedIn = VkAuthWebViewManager.hasVkSessionCookie()
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }

    LaunchedEffect(vkAuthBusy) {
        if (!vkAuthBusy) {
            vkLoggedIn = VkAuthWebViewManager.hasVkSessionCookie()
        }
    }

    LaunchedEffect(vkAccountAuth) {
        if (vkAccountAuth) {
            vkLoggedIn = VkAuthWebViewManager.hasVkSessionCookie()
        }
    }

    LaunchedEffect(savedManualPortsEnabled) {
        manualPortsEnabled = savedManualPortsEnabled
    }

    LaunchedEffect(savedServerDtlsPort) {
        serverDtlsPortInput = savedServerDtlsPort.toString()
    }

    LaunchedEffect(savedServerWgPort) {
        serverWgPortInput = savedServerWgPort.toString()
    }

    LaunchedEffect(savedListenPort) {
        portInput = savedListenPort.toString()
    }

    if (!initialized) {
        Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            CircularProgressIndicator(color = MaterialTheme.colorScheme.primary)
        }
        return
    }

    var saveJob by remember { mutableStateOf<Job?>(null) }

    fun saveTunnelSettingsNow(hashes: String = combinedHashes, onSaved: (() -> Unit)? = null) {
        saveJob?.cancel()
        scope.launch {
            val savedLocalPort = if (manualPortsEnabled) portInput.toIntOrNull()?.coerceIn(1, 65535) ?: 9000 else 9000
            val hashesList = hashes.split(Regex("[,\\s\\n]+")).filter { it.isNotBlank() && it.length >= 16 }.distinct()
            val hashesCount = hashesList.size.coerceAtLeast(1)
            val maxW = hashesCount * 27
            val finalWorkers = workersInput.toInt().coerceIn(9, maxW)
            val dtlsPort = if (manualPortsEnabled) serverDtlsPortInput.toIntOrNull()?.coerceIn(1, 65535) ?: 56000 else 56000
            val peerForTunnel = PeerAddress.ensurePort(peerInput.trim(), dtlsPort)
            settingsStore.save(
                peerForTunnel, hashes, "",
                finalWorkers, "udp", savedLocalPort, sniInput, false
            )
            onSaved?.invoke()
        }
    }

    fun scheduleSave() {
        saveJob?.cancel()
        saveJob = scope.launch {
            delay(300)
            val savedLocalPort = if (manualPortsEnabled) portInput.toIntOrNull()?.coerceIn(1, 65535) ?: 9000 else 9000
            val hashesList = combinedHashes.split(Regex("[,\\s\\n]+")).filter { it.isNotBlank() && it.length >= 16 }.distinct()
            val hashesCount = hashesList.size.coerceAtLeast(1)
            val maxW = hashesCount * 27
            val finalWorkers = workersInput.toInt().coerceIn(9, maxW)
            val dtlsPort = if (manualPortsEnabled) serverDtlsPortInput.toIntOrNull()?.coerceIn(1, 65535) ?: 56000 else 56000
            val peerForTunnel = PeerAddress.ensurePort(peerInput.trim(), dtlsPort)
            settingsStore.save(
                peerForTunnel, combinedHashes, "",
                finalWorkers, "udp", savedLocalPort, sniInput, false
            )
        }
    }

    val scrollState = rememberScrollState()

    val speedHistory = remember { mutableStateListOf<Float>() }
    var currentSpeedKbps by remember { mutableFloatStateOf(0f) }
    var lastTraffic by remember { mutableDoubleStateOf(-1.0) }
    var lastTime by remember { mutableLongStateOf(0L) }

    LaunchedEffect(tunnelRunning) {
        if (tunnelRunning) {
            speedHistory.clear()
            repeat(30) { speedHistory.add(0f) }
            lastTraffic = -1.0
            lastTime = System.currentTimeMillis()
            currentSpeedKbps = 0f
            
            while (true) {
                delay(1000)
                val now = System.currentTimeMillis()
                val statsText = TunnelManager.stats.value
                val currentTraffic = parseTrafficMb(statsText)
                
                if (currentTraffic != null) {
                    if (lastTraffic >= 0.0) {
                        val deltaTrafficMb = currentTraffic - lastTraffic
                        if (deltaTrafficMb > 0.0) {
                            val deltaTimeSec = (now - lastTime) / 1000.0
                            if (deltaTimeSec > 0) {
                                val rawSpeed = ((deltaTrafficMb * 1024.0) / deltaTimeSec).toFloat()
                                currentSpeedKbps = rawSpeed
                                lastTraffic = currentTraffic
                                lastTime = now
                            }
                        } else {
                            if (now - lastTime > 3800) {
                                currentSpeedKbps = 0f
                            }
                        }
                    } else {
                        lastTraffic = currentTraffic
                        lastTime = now
                    }
                }
                
                var speedPoint = currentSpeedKbps
                if (speedPoint > 2f) {
                    val oscillation = (Math.random() * 0.12 - 0.06).toFloat()
                    speedPoint = (speedPoint + speedPoint * oscillation).coerceAtLeast(0f)
                }
                if (speedHistory.size >= 30) speedHistory.removeAt(0)
                speedHistory.add(speedPoint)
            }
        } else {
            currentSpeedKbps = 0f
            speedHistory.clear()
        }
    }

    val isPeerValid = peerInput.isNotBlank()
    val isHashesValid = combinedHashes.isNotBlank()
    val isValid = isPeerValid && isHashesValid && savedConnectionPassword.isNotBlank() && !hasInputHashErrors
    val effectiveServerDtlsPort = if (manualPortsEnabled) serverDtlsPortInput.toIntOrNull()?.coerceIn(1, 65535) ?: 56000 else 56000
    val effectiveLocalPort = if (manualPortsEnabled) portInput.toIntOrNull()?.coerceIn(1, 65535) ?: 9000 else 9000
    var pendingStartAfterVpnPermission by remember { mutableStateOf(false) }

    fun startTunnelService() {
        val effectiveCaptchaMode = if (autoCaptchaEnabled) "auto" else if (useWVCaptcha) "wv" else "rjs"
        val effectiveCaptchaSolveMethod = if (!autoCaptchaEnabled && effectiveCaptchaMode == "wv" && isManualMode) "manual" else "auto"
        val hashesList = combinedHashes.split(Regex("[,\\s\\n]+")).filter { it.isNotBlank() && it.length >= 16 }.distinct()
        val hashesCount = hashesList.size.coerceAtLeast(1)
        val maxW = hashesCount * 27
        val finalWorkers = workersInput.toInt().coerceIn(9, maxW)
        val peerForTunnel = PeerAddress.ensurePort(peerInput.trim(), effectiveServerDtlsPort)
        saveJob?.cancel()
        scope.launch {
            settingsStore.save(
                peerForTunnel, combinedHashes, "",
                finalWorkers, "udp", effectiveLocalPort, sniInput, false
            )
            settingsStore.saveCaptchaMode(effectiveCaptchaMode)
            settingsStore.saveCaptchaSolveMethod(effectiveCaptchaSolveMethod)
        }
        val intent = Intent(context, TunnelService::class.java).apply {
            action = "START"
            putExtra("peer", peerForTunnel)
            putExtra("vk_hashes", combinedHashes)
            putExtra("secondary_vk_hash", "")
            putExtra("workers_per_hash", finalWorkers)
            putExtra("port", effectiveLocalPort)
            putExtra("sni", sniInput)
            putExtra("connection_password", savedConnectionPassword)
            putExtra("captcha_mode", effectiveCaptchaMode)
            putExtra("captcha_solve_method", effectiveCaptchaSolveMethod)
            putExtra("vk_auth_mode", if (vkAccountAuth) "account" else "anonymous")
        }
        if (Build.VERSION.SDK_INT >= 26) context.startForegroundService(intent)
        else context.startService(intent)
    }

    val vpnPermissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) {
        if (pendingStartAfterVpnPermission) {
            pendingStartAfterVpnPermission = false
            if (VpnService.prepare(context) == null) {
                startTunnelService()
            } else {
                Toast.makeText(context, "VPN-разрешение не выдано", Toast.LENGTH_SHORT).show()
            }
        }
    }

    fun requestVpnAndStart() {
        val vpnIntent = VpnService.prepare(context)
        if (vpnIntent != null) {
            pendingStartAfterVpnPermission = true
            vpnPermissionLauncher.launch(vpnIntent)
        } else {
            startTunnelService()
        }
    }

    // ═══ Dialogs ═══
    if (showSecretsDialog) {
        SecretsDialog(
            settingsStore = settingsStore,
            initialPassword = savedConnectionPassword,
            manualPortsEnabled = manualPortsEnabled,
            initialServerDtlsPort = serverDtlsPortInput,
            initialServerWgPort = serverWgPortInput,
            initialLocalPort = portInput,
            onSaved = { dtls, wg, local ->
                serverDtlsPortInput = dtls
                serverWgPortInput = wg
                portInput = local
            },
            onDismiss = { showSecretsDialog = false }
        )
    }

    if (showHashesDialog) {
        val activeParts = currentHashesRaw.split(Regex("[,\\s\\n]+")).filter { it.isNotEmpty() }
        HashesDialog(
            hash1 = activeParts.getOrElse(0) { "" },
            hash2 = activeParts.getOrElse(1) { "" },
            hash3 = activeParts.getOrElse(2) { "" },
            hash4 = activeParts.getOrElse(3) { "" },
            onSave = { h1, h2, h3, h4 ->
                val cleaned1 = stripVkUrlStatic(h1)
                val cleaned2 = stripVkUrlStatic(h2)
                val cleaned3 = stripVkUrlStatic(h3)
                val cleaned4 = stripVkUrlStatic(h4)
                val combined = normalizeHashes(cleaned1, cleaned2, cleaned3, cleaned4)
                
                scope.launch {
                    val currentProfileIdStr = settingsStore.currentProfileId.first()
                    val currentProfile = profiles.firstOrNull { it.id == currentProfileIdStr }
                    
                    if (currentProfileIdStr.isEmpty() || (currentProfile != null && currentProfile.useGlobalHashes)) {
                        settingsStore.saveGlobalVkHashes(combined)
                    }
                    
                    // Coerce workers count to new max immediately!
                    val newHashCount = combined.split(",").filter { it.isNotBlank() && it.length >= 16 }.size.coerceAtLeast(1)
                    val newMax = newHashCount * 27
                    if (workersInput > newMax) {
                        workersInput = newMax.toFloat()
                    }
                    
                    saveTunnelSettingsNow(combined) { showHashesDialog = false }
                }
            },
            onDismiss = { showHashesDialog = false }
        )
    }

    if (showAppSettingsDialog) {
        AlertDialog(
            onDismissRequest = { showAppSettingsDialog = false },
            title = {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text("Настройки", style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.Bold))
                    IconButton(onClick = { showAppSettingsDialog = false }) {
                        Icon(Icons.Default.Close, contentDescription = "Закрыть")
                    }
                }
            },
            text = {
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(16.dp)
                ) {
                    // ═══ Раздел: Оформление ═══
                    Text(
                        "Оформление",
                        style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.Bold),
                        color = MaterialTheme.colorScheme.primary
                    )

                    Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
                        // Тема оформления
                        Column(
                            modifier = Modifier.fillMaxWidth(),
                            verticalArrangement = Arrangement.spacedBy(8.dp)
                        ) {
                            Text(
                                "Тема оформления",
                                style = MaterialTheme.typography.bodyMedium,
                                fontWeight = FontWeight.SemiBold
                            )
                            
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                horizontalArrangement = Arrangement.spacedBy(8.dp)
                            ) {
                                ProtocolChip(
                                    label = "Сист.",
                                    selected = themeMode == "system",
                                    enabled = true,
                                    isError = false,
                                    modifier = Modifier.weight(1f)
                                ) {
                                    onThemeChange("system")
                                }
                                ProtocolChip(
                                    label = "Свет.",
                                    selected = themeMode == "light",
                                    enabled = true,
                                    isError = false,
                                    modifier = Modifier.weight(1f)
                                ) {
                                    onThemeChange("light")
                                }
                                ProtocolChip(
                                    label = "Темн.",
                                    selected = themeMode == "dark",
                                    enabled = true,
                                    isError = false,
                                    modifier = Modifier.weight(1f)
                                ) {
                                    onThemeChange("dark")
                                }
                            }
                        }

                        val supportsDynamicColor = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S
                        if (supportsDynamicColor) {
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.SpaceBetween
                            ) {
                                Column(modifier = Modifier.weight(1f).padding(end = 16.dp)) {
                                    Text(
                                        "Динамические цвета",
                                        style = MaterialTheme.typography.bodyMedium,
                                        fontWeight = FontWeight.SemiBold
                                    )
                                    Text(
                                        "Material You",
                                        style = MaterialTheme.typography.bodySmall,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant
                                    )
                                }
                                Switch(
                                    checked = isDynamicColor,
                                    onCheckedChange = { onDynamicColorChange(it) },
                                    modifier = Modifier.scale(0.8f)
                                )
                            }
                        }

                        // Выбор палитры, если динамические цвета выключены
                        if (!isDynamicColor || !supportsDynamicColor) {
                            Column(
                                modifier = Modifier.fillMaxWidth(),
                                verticalArrangement = Arrangement.spacedBy(8.dp)
                            ) {
                                Text(
                                    "Цветовая палитра",
                                    style = MaterialTheme.typography.bodyMedium,
                                    fontWeight = FontWeight.SemiBold
                                )
                                
                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    horizontalArrangement = Arrangement.spacedBy(16.dp),
                                    verticalAlignment = Alignment.CenterVertically
                                ) {
                                    PaletteCircleOption("indigo", 0xFF5B588D, currentPalette, onPaletteChange)
                                    PaletteCircleOption("forest", 0xFF5F5D68, currentPalette, onPaletteChange)
                                    PaletteCircleOption("espresso", 0xFF6D4C41, currentPalette, onPaletteChange)
                                }
                            }
                        }
                    }

                    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f))

                    // ═══ Раздел: Поведение ═══
                    Text(
                        "Поведение",
                        style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.Bold),
                        color = MaterialTheme.colorScheme.primary
                    )

                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.SpaceBetween
                    ) {
                        Column(modifier = Modifier.weight(1f).padding(end = 16.dp)) {
                            Text(
                                "Логи при подключении",
                                style = MaterialTheme.typography.bodyMedium,
                                fontWeight = FontWeight.Medium
                            )
                            Text(
                                "Переключаться на вкладку «Логи» при запуске туннеля",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant
                            )
                        }
                        Switch(
                            checked = autoSwitchToLogs,
                            onCheckedChange = { enabled ->
                                scope.launch { settingsStore.saveAutoSwitchToLogs(enabled) }
                            }
                        )
                    }
                    
                    Spacer(modifier = Modifier.height(10.dp))

                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.SpaceBetween
                    ) {
                        Column(modifier = Modifier.weight(1f).padding(end = 16.dp)) {
                            Text(
                                "График скорости",
                                style = MaterialTheme.typography.bodyMedium,
                                fontWeight = FontWeight.Medium
                            )
                            Text(
                                "Отображать график скорости на вкладке туннеля при активном соединении",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant
                            )
                        }
                        Switch(
                            checked = showSpeedGraph,
                            onCheckedChange = { enabled ->
                                scope.launch { settingsStore.saveShowSpeedGraph(enabled) }
                            }
                        )
                    }

                    Spacer(modifier = Modifier.height(10.dp))

                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.SpaceBetween
                    ) {
                        Column(modifier = Modifier.weight(1f).padding(end = 16.dp)) {
                            Text(
                                "Подробные логи",
                                style = MaterialTheme.typography.bodyMedium,
                                fontWeight = FontWeight.Medium
                            )
                            Text(
                                "Записывать больше диагностической информации (замедляет работу)",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant
                            )
                        }
                        Switch(
                            checked = detailedLogs,
                            onCheckedChange = { enabled ->
                                scope.launch { settingsStore.saveDetailedLogs(enabled) }
                            }
                        )
                    }

                    // Removed BS check toggle

                    Spacer(modifier = Modifier.height(10.dp))

                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.SpaceBetween
                    ) {
                        Column(modifier = Modifier.weight(1f).padding(end = 16.dp)) {
                            Text(
                                "Проверять обновления",
                                style = MaterialTheme.typography.bodyMedium,
                                fontWeight = FontWeight.Medium
                            )
                            Text(
                                "Автоматически проверять наличие обновлений при открытии приложения",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant
                            )
                        }
                        Switch(
                            checked = updateCheckIntervalHours != com.wdtt.client.UPDATE_CHECK_NEVER,
                            onCheckedChange = { enabled ->
                                scope.launch {
                                    val newInterval = if (enabled) {
                                        com.wdtt.client.DEFAULT_UPDATE_CHECK_INTERVAL_HOURS
                                    } else {
                                        com.wdtt.client.UPDATE_CHECK_NEVER
                                    }
                                    settingsStore.saveUpdateCheckIntervalHours(newInterval)
                                }
                            }
                        )
                    }

                    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f))

                    // ═══ Раздел: О приложении ═══
                    Text(
                        "О приложении",
                        style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.Bold),
                        color = MaterialTheme.colorScheme.primary
                    )

                    val currentVersion = remember { "v${com.wdtt.client.BuildConfig.VERSION_NAME.removePrefix("v")}" }
                    var isCheckingUpdates by remember { mutableStateOf(false) }
                    val updateLatestVersion by settingsStore.updateLatestVersion.collectAsStateWithLifecycle(initialValue = "")
                    val updateLastError by settingsStore.updateLastError.collectAsStateWithLifecycle(initialValue = "")

                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.SpaceBetween
                        ) {
                            Column {
                                Text(
                                    text = "qWDTT",
                                    style = MaterialTheme.typography.bodyLarge,
                                    fontWeight = FontWeight.Bold
                                )
                                Text(
                                    text = "Версия $currentVersion",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant
                                )
                            }
                            
                            Row(
                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                                verticalAlignment = Alignment.CenterVertically
                            ) {
                                Button(
                                    onClick = {
                                        val intent = Intent(Intent.ACTION_VIEW, Uri.parse("https://t.me/darkbitVPN"))
                                        context.startActivity(intent)
                                    },
                                    shape = RoundedCornerShape(8.dp),
                                    colors = ButtonDefaults.buttonColors(
                                        containerColor = MaterialTheme.colorScheme.primaryContainer,
                                        contentColor = MaterialTheme.colorScheme.onPrimaryContainer
                                    ),
                                    contentPadding = PaddingValues(horizontal = 8.dp, vertical = 4.dp)
                                ) {
                                    Text("Telegram", style = MaterialTheme.typography.labelMedium)
                                }

                                OutlinedButton(
                                    onClick = {
                                        val intent = Intent(Intent.ACTION_VIEW, Uri.parse("https://github.com/SpaceNeuroX/proxy-turn-vk-android"))
                                        context.startActivity(intent)
                                    },
                                    shape = RoundedCornerShape(8.dp),
                                    contentPadding = PaddingValues(horizontal = 8.dp, vertical = 4.dp)
                                ) {
                                    Text("GitHub", style = MaterialTheme.typography.labelMedium)
                                }
                            }
                        }

                        Text(
                            text = "Форк оригинального проекта amurcanov/proxy-turn-vk-android от разработчика SpaceNeuroX.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                        
                        Spacer(modifier = Modifier.height(8.dp))
                        
                        Surface(
                            shape = RoundedCornerShape(16.dp),
                            color = MaterialTheme.colorScheme.secondaryContainer.copy(alpha = 0.4f),
                            modifier = Modifier.fillMaxWidth()
                        ) {
                            Column(
                                modifier = Modifier.padding(16.dp),
                                verticalArrangement = Arrangement.spacedBy(12.dp)
                            ) {
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    Icon(
                                        imageVector = androidx.compose.material.icons.Icons.Filled.Info,
                                        contentDescription = null,
                                        tint = MaterialTheme.colorScheme.primary,
                                        modifier = Modifier.size(20.dp)
                                    )
                                    Spacer(modifier = Modifier.width(8.dp))
                                    Text(
                                        "Готовые профили",
                                        style = MaterialTheme.typography.titleMedium,
                                        fontWeight = FontWeight.Bold,
                                        color = MaterialTheme.colorScheme.onSecondaryContainer
                                    )
                                }
                                
                                Text(
                                    "Вы можете получить готовые конфиги напрямую в этих Telegram-ботах:",
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant
                                )

                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    horizontalArrangement = Arrangement.spacedBy(8.dp)
                                ) {
                                    Button(
                                        onClick = {
                                            val intent = android.content.Intent(android.content.Intent.ACTION_VIEW, android.net.Uri.parse("https://t.me/darkbit_vpnbot"))
                                            context.startActivity(intent)
                                        },
                                        modifier = Modifier.weight(1f),
                                        colors = ButtonDefaults.buttonColors(
                                            containerColor = MaterialTheme.colorScheme.surface,
                                            contentColor = MaterialTheme.colorScheme.primary
                                        ),
                                        elevation = ButtonDefaults.buttonElevation(defaultElevation = 2.dp),
                                        shape = RoundedCornerShape(12.dp),
                                        contentPadding = PaddingValues(vertical = 10.dp)
                                    ) {
                                        Text("🤖 @darkbit_vpnbot", maxLines = 1, style = MaterialTheme.typography.labelMedium)
                                    }
                                    
                                    Button(
                                        onClick = {
                                            val intent = android.content.Intent(android.content.Intent.ACTION_VIEW, android.net.Uri.parse("https://t.me/sidylinkbot"))
                                            context.startActivity(intent)
                                        },
                                        modifier = Modifier.weight(1f),
                                        colors = ButtonDefaults.buttonColors(
                                            containerColor = MaterialTheme.colorScheme.surface,
                                            contentColor = MaterialTheme.colorScheme.primary
                                        ),
                                        elevation = ButtonDefaults.buttonElevation(defaultElevation = 2.dp),
                                        shape = RoundedCornerShape(12.dp),
                                        contentPadding = PaddingValues(vertical = 10.dp)
                                    ) {
                                        Text("🤖 @sidylinkbot", maxLines = 1, style = MaterialTheme.typography.labelMedium)
                                    }
                                }
                            }
                        }

                        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.3f))

                        // Проверка обновлений
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.SpaceBetween
                        ) {
                            val updateStatusText = remember(isCheckingUpdates, updateLatestVersion, updateLastError) {
                                when {
                                    isCheckingUpdates -> "Проверяем..."
                                    updateLatestVersion.isNotBlank() && isNewerVersion(currentVersion, updateLatestVersion) -> "Доступна $updateLatestVersion!"
                                    updateLatestVersion.isNotBlank() -> "Обновлений нет"
                                    updateLastError.isNotBlank() -> "Ошибка"
                                    else -> "Не проверено"
                                }
                            }
                            
                            Column(modifier = Modifier.weight(1f).padding(end = 16.dp)) {
                                Text(
                                    "Обновления",
                                    style = MaterialTheme.typography.bodyMedium,
                                    fontWeight = FontWeight.Medium
                                )
                                Text(
                                    text = updateStatusText,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = if (updateLatestVersion.isNotBlank() && isNewerVersion(currentVersion, updateLatestVersion)) {
                                        MaterialTheme.colorScheme.primary
                                    } else {
                                        MaterialTheme.colorScheme.onSurfaceVariant
                                    }
                                )
                            }

                            Button(
                                onClick = {
                                    scope.launch {
                                        isCheckingUpdates = true
                                        try {
                                            val release = com.wdtt.client.fetchLatestReleaseInfo(currentVersion)
                                            if (release != null) {
                                                settingsStore.saveUpdateState(
                                                    lastCheckAt = System.currentTimeMillis(),
                                                    latestVersion = release.versionTag,
                                                    error = ""
                                                )
                                                if (isNewerVersion(currentVersion, release.versionTag)) {
                                                    Toast.makeText(context, "Доступна новая версия: ${release.versionTag}", Toast.LENGTH_LONG).show()
                                                } else {
                                                    Toast.makeText(context, "У вас последняя версия!", Toast.LENGTH_SHORT).show()
                                                }
                                            } else {
                                                settingsStore.saveUpdateState(
                                                    lastCheckAt = System.currentTimeMillis(),
                                                    latestVersion = "",
                                                    error = "Ошибка"
                                                )
                                                Toast.makeText(context, "Не удалось проверить обновления", Toast.LENGTH_SHORT).show()
                                            }
                                        } catch (e: java.lang.Exception) {
                                            Toast.makeText(context, "Ошибка: ${e.message}", Toast.LENGTH_SHORT).show()
                                        } finally {
                                            isCheckingUpdates = false
                                        }
                                    }
                                },
                                enabled = !isCheckingUpdates,
                                shape = RoundedCornerShape(8.dp),
                                contentPadding = PaddingValues(horizontal = 8.dp, vertical = 4.dp)
                            ) {
                                Text("Проверить", style = MaterialTheme.typography.labelMedium)
                            }
                        }

                        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.3f))

                        // Копия отчета
                        OutlinedButton(
                            onClick = {
                                val reportText = """
                                    Приложение: qWDTT
                                    Версия: $currentVersion
                                    Android API: ${Build.VERSION.SDK_INT}
                                    Архитектура (ABI): ${Build.SUPPORTED_ABIS.firstOrNull() ?: "unknown"}
                                    Устройство: ${Build.MANUFACTURER} ${Build.MODEL}
                                """.trimIndent()
                                val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager
                                clipboard.setPrimaryClip(ClipData.newPlainText("qWDTT Report", reportText))
                                Toast.makeText(context, "Отчёт о системе скопирован!", Toast.LENGTH_SHORT).show()
                            },
                            modifier = Modifier.fillMaxWidth(),
                            shape = RoundedCornerShape(8.dp)
                        ) {
                            Text("Скопировать системный отчёт")
                        }

                        Spacer(Modifier.height(12.dp))
                    }
                }
            },
            confirmButton = {
                TextButton(onClick = { showAppSettingsDialog = false }) {
                    Text("Готово")
                }
            },
            properties = DialogProperties(usePlatformDefaultWidth = false),
            modifier = Modifier.padding(16.dp).fillMaxWidth()
        )
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(scrollState)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)
    ) {
        // ═══ Топ-тулбар с заголовком и иконкой настроек ═══
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(vertical = 4.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically
        ) {
            Text(
                text = "qWDTT",
                style = MaterialTheme.typography.headlineMedium.copy(fontWeight = FontWeight.ExtraBold),
                color = MaterialTheme.colorScheme.primary
            )
            
            // Иконка настроек (шестеренка)
            IconButton(
                onClick = { showAppSettingsDialog = true }
            ) {
                Icon(
                    imageVector = Icons.Default.Settings,
                    contentDescription = "Настройки оформления и инфо",
                    tint = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }

        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.3f))

        // ═══ График скорости при активном туннеле ═══
        androidx.compose.animation.AnimatedVisibility(
            visible = tunnelRunning && showSpeedGraph,
            enter = androidx.compose.animation.fadeIn() + androidx.compose.animation.expandVertically(),
            exit = androidx.compose.animation.fadeOut() + androidx.compose.animation.shrinkVertically()
        ) {
            SpeedGraphCard(speedHistory = speedHistory, currentSpeed = currentSpeedKbps)
        }

        // ═══ Заголовок раздела ═══
        Text(
            "Настройки туннеля",
            style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.Bold),
            color = MaterialTheme.colorScheme.onSurface
        )

        // ═══ Настройки туннеля ═══
        AppSectionCard(
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            // Быстрый выбор профиля
            if (profiles.isNotEmpty()) {
                var expanded by remember { mutableStateOf(false) }
                Box(modifier = Modifier.fillMaxWidth()) {
                    OutlinedButton(
                        onClick = { expanded = true },
                        modifier = Modifier.fillMaxWidth().height(52.dp),
                        shape = RoundedCornerShape(16.dp),
                        colors = ButtonDefaults.outlinedButtonColors(
                            containerColor = MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.15f),
                            contentColor = MaterialTheme.colorScheme.primary
                        ),
                        border = BorderStroke(
                            1.dp,
                            MaterialTheme.colorScheme.primary.copy(alpha = 0.4f)
                        )
                    ) {
                        Icon(
                            imageVector = Icons.Default.PlayArrow,
                            contentDescription = null,
                            modifier = Modifier.size(18.dp)
                        )
                        Spacer(Modifier.width(8.dp))
                        Text(
                            text = if (currentProfileName.isNotEmpty()) "Профиль: $currentProfileName" else "Быстрый выбор профиля",
                            fontWeight = FontWeight.Bold,
                            style = MaterialTheme.typography.bodyMedium
                        )
                    }
                    
                    DropdownMenu(
                        expanded = expanded,
                        onDismissRequest = { expanded = false },
                        modifier = Modifier
                            .fillMaxWidth(0.85f)
                            .border(
                                width = 1.dp,
                                color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.6f),
                                shape = RoundedCornerShape(20.dp)
                            )
                            .clip(RoundedCornerShape(20.dp))
                            .background(MaterialTheme.colorScheme.surface)
                    ) {
                        profiles.forEach { p ->
                            DropdownMenuItem(
                                text = {
                                    Text(
                                        p.name,
                                        fontWeight = if (p.id == currentProfileId) FontWeight.Bold else FontWeight.Normal,
                                        color = if (p.id == currentProfileId) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface
                                    )
                                },
                                onClick = {
                                    expanded = false
                                    scope.launch {
                                        profilesStore.applyProfile(context, p.id)

                                        peerInput = settingsStore.peer.first()
                                        portInput = settingsStore.listenPort.first().toString()
                                        workersInput = roundToGroup(
                                            settingsStore.workersPerHash.first().toFloat(),
                                            dynamicMaxWorkers
                                        )

                                        if (tunnelRunning) {
                                            context.startService(
                                                Intent(context, TunnelService::class.java).apply { action = "STOP" }
                                            )
                                            kotlinx.coroutines.delay(800)
                                            requestVpnAndStart()
                                        }

                                        Toast.makeText(context, "Профиль «${p.name}» применен!", Toast.LENGTH_SHORT).show()
                                    }
                                },
                                leadingIcon = {
                                    Icon(
                                        imageVector = Icons.Default.PlayArrow,
                                        contentDescription = null,
                                        tint = if (p.id == currentProfileId) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant
                                    )
                                }
                            )
                        }
                    }
                }
                
                HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f))
            }
            OutlinedTextField(
                value = peerInput,
                onValueChange = {
                    peerInput = it.filter { c -> c != ' ' }
                    scheduleSave()
                },
                label = { Text("IP сервера или домен (с портом или без)") },
                placeholder = { Text("1.2.3.4 или 1.2.3.4:56000") },
                singleLine = true,
                isError = !isPeerValid && peerInput.isNotEmpty(),
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(16.dp),
                colors = OutlinedTextFieldDefaults.colors(
                    focusedBorderColor = MaterialTheme.colorScheme.primary,
                    unfocusedBorderColor = MaterialTheme.colorScheme.outline.copy(alpha = 0.3f),
                )
            )

            OutlinedButton(
                onClick = { showHashesDialog = true },
                modifier = Modifier.fillMaxWidth().height(56.dp),
                shape = RoundedCornerShape(16.dp),
                colors = ButtonDefaults.outlinedButtonColors(
                    containerColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.5f),
                    contentColor = MaterialTheme.colorScheme.onSurface
                ),
                border = BorderStroke(
                    1.dp,
                    if (hasInputHashErrors) MaterialTheme.colorScheme.error
                    else MaterialTheme.colorScheme.outline.copy(alpha = 0.5f)
                )
            ) {
                Icon(Icons.Default.Tag, null, Modifier.size(18.dp))
                Spacer(Modifier.width(8.dp))
                Text("VK Хеши ($filledHashCount)", fontWeight = FontWeight.SemiBold)
            }

            val errorTexts = hashErrors.filter { !it.contains("короткий") }
            if (errorTexts.isNotEmpty()) {
                Text(
                    text = errorTexts.joinToString(", "),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error
                )
            }
        }

        // ═══ Мощность + Капча ═══
        AppSectionCard(
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(0.dp)
        ) {
                // — Мощность —
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text(
                        "Мощность",
                        style = MaterialTheme.typography.titleMedium,
                        color = MaterialTheme.colorScheme.primary,
                        fontWeight = FontWeight.SemiBold
                    )
                    Text(
                        text = "${currentWorkers.toInt()}",
                        style = MaterialTheme.typography.titleMedium.copy(fontWeight = FontWeight.Bold),
                        color = MaterialTheme.colorScheme.primary
                    )
                }

                Spacer(Modifier.height(4.dp))

                val maxWorkers = dynamicMaxWorkers
                val minWorkers = if (vkAccountAuth) 1f else WORKERS_PER_GROUP.toFloat()
                val workerStep = if (vkAccountAuth) 1f else WORKERS_PER_GROUP.toFloat()
                val currentWorkersVal = if (vkAccountAuth) {
                    currentWorkers.coerceIn(1f, maxWorkers).roundToInt().toFloat()
                } else {
                    roundToGroup(currentWorkers.coerceIn(minWorkers, maxWorkers), maxWorkers)
                }

                CompactSteppedSlider(
                    value = currentWorkersVal,
                    onValueChange = { raw ->
                        workersInput = if (vkAccountAuth) {
                            raw.coerceIn(1f, maxWorkers).roundToInt().toFloat()
                        } else {
                            roundToGroup(raw, maxWorkers)
                        }
                        scheduleSave()
                    },
                    valueRange = minWorkers..maxWorkers,
                    stepSize = workerStep,
                    enabled = !tunnelRunning,
                    modifier = Modifier.fillMaxWidth()
                )

                // — Разделитель —
                HorizontalDivider(
                    modifier = Modifier.padding(vertical = 4.dp),
                    color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f)
                )

                Row(
                    modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.SpaceBetween
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            "Вход через аккаунт VK",
                            style = MaterialTheme.typography.bodyMedium,
                            fontWeight = FontWeight.Medium
                        )
                        Text(
                            "Если анонимный режим не работает — включите и войдите в свой аккаунт VK. Подключение стабильнее.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                    }
                    Switch(
                        checked = vkAccountAuth,
                        enabled = !tunnelRunning && !vkAuthBusy,
                        onCheckedChange = { enabled ->
                            vkAccountAuth = enabled
                            scope.launch {
                                settingsStore.saveVkAuthMode(if (enabled) "account" else "anonymous")
                            }
                        }
                    )
                }

                if (vkAccountAuth) {
                    Button(
                        onClick = {
                            scope.launch {
                                vkAuthBusy = true
                                try {
                                    val result = VkAuthWebViewManager.loginOnly(context)
                                    result.onSuccess {
                                        vkLoggedIn = true
                                        Toast.makeText(
                                            context,
                                            "Вход в VK выполнен",
                                            Toast.LENGTH_SHORT
                                        ).show()
                                    }.onFailure {
                                        vkLoggedIn = VkAuthWebViewManager.hasVkSessionCookie()
                                        Toast.makeText(
                                            context,
                                            "VK: ${it.message ?: "ошибка"}",
                                            Toast.LENGTH_LONG
                                        ).show()
                                    }
                                } finally {
                                    vkAuthBusy = false
                                }
                            }
                        },
                        enabled = !tunnelRunning && !vkAuthBusy,
                        modifier = Modifier.fillMaxWidth()
                    ) {
                        Text(if (vkAuthBusy) "Ожидание входа VK..." else "Войти в VK")
                    }
                    if (vkLoggedIn) {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(top = 8.dp, bottom = 4.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(6.dp)
                        ) {
                            Icon(
                                Icons.Default.CheckCircle,
                                contentDescription = null,
                                tint = Color(0xFF43A047),
                                modifier = Modifier.size(18.dp)
                            )
                            Text(
                                "Вход в VK выполнен",
                                style = MaterialTheme.typography.bodySmall,
                                color = Color(0xFF43A047),
                                fontWeight = FontWeight.Medium
                            )
                        }
                    } else {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(top = 8.dp, bottom = 4.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(6.dp)
                        ) {
                            Icon(
                                Icons.Default.Info,
                                contentDescription = null,
                                tint = MaterialTheme.colorScheme.tertiary,
                                modifier = Modifier.size(18.dp)
                            )
                            Text(
                                "Вход в VK не выполнен",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.tertiary,
                                fontWeight = FontWeight.Medium
                            )
                        }
                    }
                }

                HorizontalDivider(
                    modifier = Modifier.padding(vertical = 4.dp),
                    color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f)
                )

                // — Авто капча —
                AnimatedVisibility(visible = !vkAccountAuth) {
                Column {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.SpaceBetween
                ) {
                    Text(
                        if (autoCaptchaEnabled) "Авто капча" else "Ручная капча",
                        style = MaterialTheme.typography.bodyMedium,
                        fontWeight = FontWeight.Medium,
                        modifier = Modifier.weight(1f)
                    )
                    Switch(
                        checked = autoCaptchaEnabled,
                        onCheckedChange = { enabled ->
                            autoCaptchaEnabled = enabled
                            scope.launch {
                                if (enabled) {
                                    settingsStore.saveCaptchaMode("auto")
                                    settingsStore.saveCaptchaSolveMethod("auto")
                                } else {
                                    val mode = if (useWVCaptcha) "wv" else "rjs"
                                    settingsStore.saveCaptchaMode(mode)
                                    settingsStore.saveCaptchaSolveMethod(if (mode == "wv" && isManualMode) "manual" else "auto")
                                }
                            }
                        }
                    )
                }

                AnimatedVisibility(
                    visible = !autoCaptchaEnabled,
                    enter = fadeIn() + expandVertically(),
                    exit = fadeOut() + shrinkVertically()
                ) {
                    Column(verticalArrangement = Arrangement.spacedBy(0.dp)) {
                        // — Разделитель —
                        HorizontalDivider(
                            modifier = Modifier.padding(vertical = 4.dp),
                            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f)
                        )

                        // — Метод обхода капчи —
                        Row(
                            modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.SpaceBetween
                        ) {
                            Text(
                                "Метод обхода капчи",
                                style = MaterialTheme.typography.bodyMedium,
                                fontWeight = FontWeight.Medium,
                                modifier = Modifier.weight(1f)
                            )
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                ProtocolChip("WBV", useWVCaptcha, enabled = true) {
                                    useWVCaptcha = true
                                    isManualMode = wbvManualMode
                                    scope.launch {
                                        settingsStore.saveCaptchaMode("wv")
                                        settingsStore.saveCaptchaSolveMethod(if (wbvManualMode) "manual" else "auto")
                                    }
                                }
                                ProtocolChip("RJS", !useWVCaptcha, enabled = true, isError = false) {
                                    useWVCaptcha = false
                                    isManualMode = false
                                    scope.launch {
                                        settingsStore.saveCaptchaMode("rjs")
                                        settingsStore.saveCaptchaSolveMethod("auto")
                                    }
                                }
                            }
                        }

                        // — Разделитель —
                        HorizontalDivider(
                            modifier = Modifier.padding(vertical = 4.dp),
                            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f)
                        )

                        // — Режим обхода —
                        Row(
                            modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.SpaceBetween
                        ) {
                            Text(
                                "Режим обхода",
                                style = MaterialTheme.typography.bodyMedium,
                                fontWeight = FontWeight.Medium,
                                modifier = Modifier.weight(1f)
                            )
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                if (useWVCaptcha) {
                                    ProtocolChip(
                                        "РУЧ",
                                        isManualMode,
                                        enabled = true,
                                        isError = false
                                    ) {
                                        isManualMode = true
                                        wbvManualMode = true
                                        scope.launch { settingsStore.saveWbvCaptchaSolveMethod("manual") }
                                    }
                                    ProtocolChip(
                                        "АВТ",
                                        !isManualMode,
                                        enabled = true,
                                        isError = false
                                    ) {
                                        isManualMode = false
                                        wbvManualMode = false
                                        scope.launch { settingsStore.saveWbvCaptchaSolveMethod("auto") }
                                    }
                                } else {
                                    ProtocolChip(
                                        "АВТ",
                                        selected = true,
                                        enabled = true,
                                        isError = false
                                    ) {}
                                }
                            }
                        }
                    }
                }
                }
                }
        }

        // ═══ Кнопки: Секреты + Подключить ═══
        val tunnelSecretsMissing = savedConnectionPassword.isBlank()
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            OutlinedButton(
                onClick = { showSecretsDialog = true },
                modifier = Modifier.height(52.dp),
                shape = RoundedCornerShape(16.dp),
                colors = ButtonDefaults.outlinedButtonColors(
                    containerColor = if (tunnelSecretsMissing) MaterialTheme.colorScheme.errorContainer else MaterialTheme.colorScheme.surface,
                    contentColor = if (tunnelSecretsMissing) MaterialTheme.colorScheme.onErrorContainer else MaterialTheme.colorScheme.onSurface
                ),
                border = BorderStroke(
                    1.dp,
                    if (tunnelSecretsMissing) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.outline.copy(alpha = 0.5f)
                )
            ) {
                Icon(imageVector = Icons.Default.Key, contentDescription = null, modifier = Modifier.size(18.dp))
                Spacer(modifier = Modifier.width(8.dp))
                Text("Секреты", fontWeight = FontWeight.SemiBold)
            }

            val buttonColor by animateColorAsState(
                targetValue = if (tunnelRunning) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary,
                animationSpec = tween(400),
                label = "btn_color"
            )

            Button(
                onClick = {
                    if (tunnelRunning) {
                        context.startService(
                            Intent(context, TunnelService::class.java).apply { action = "STOP" }
                        )
                    } else {
                        requestVpnAndStart()
                        if (autoSwitchToLogs) {
                            onNavigateToLogs()
                        }
                    }
                },
                enabled = (isValid && cooldownSeconds == 0) || tunnelRunning,
                modifier = Modifier.weight(1f).height(52.dp),
                shape = RoundedCornerShape(16.dp),
                colors = ButtonDefaults.buttonColors(
                    containerColor = buttonColor,
                    contentColor = MaterialTheme.colorScheme.onPrimary
                )
            ) {
                Icon(
                    imageVector = if (tunnelRunning) Icons.Default.Stop else Icons.Default.PowerSettingsNew,
                    contentDescription = null,
                    modifier = Modifier.size(20.dp)
                )
                Spacer(Modifier.width(8.dp))
                Text(
                    text = when {
                        tunnelRunning -> "Остановить"
                        cooldownSeconds > 0 -> "Подождите ($cooldownSeconds)"
                        else -> "Подключить"
                    },
                    fontWeight = FontWeight.Bold
                )
            }
        }

    }
}

// ═══ Reusable mode chip ═══
@Composable
private fun ProtocolChip(
    label: String,
    selected: Boolean,
    enabled: Boolean = true,
    isError: Boolean = false,
    modifier: Modifier = Modifier,
    onClick: () -> Unit
) {
    FilterChip(
        selected = selected,
        onClick = onClick,
        enabled = enabled,
        modifier = modifier,
        label = {
            Box(
                modifier = Modifier,
                contentAlignment = Alignment.Center
            ) {
                Text(
                    label,
                    style = MaterialTheme.typography.labelSmall,
                    fontWeight = if (selected) FontWeight.Bold else FontWeight.Medium,
                    color = if (isError) MaterialTheme.colorScheme.error else (if (selected) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurface),
                    maxLines = 1,
                    overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis
                )
            }
        },
        shape = RoundedCornerShape(16.dp),
        colors = FilterChipDefaults.filterChipColors(
            selectedContainerColor = MaterialTheme.colorScheme.primary,
            selectedLabelColor = MaterialTheme.colorScheme.onPrimary,
            containerColor = MaterialTheme.colorScheme.surfaceVariant,
            labelColor = MaterialTheme.colorScheme.onSurface,
            disabledLabelColor = if (isError) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.38f)
        ),
        border = FilterChipDefaults.filterChipBorder(
            enabled = true,
            selected = selected,
            borderColor = MaterialTheme.colorScheme.outline.copy(alpha = 0.4f),
            selectedBorderColor = MaterialTheme.colorScheme.primary
        )
    )
}

@Composable
private fun CompactSteppedSlider(
    value: Float,
    onValueChange: (Float) -> Unit,
    valueRange: ClosedFloatingPointRange<Float>,
    stepSize: Float,
    enabled: Boolean,
    modifier: Modifier = Modifier
) {
    val activeColor = MaterialTheme.colorScheme.primary.copy(alpha = if (enabled) 1f else 0.38f)
    val inactiveColor = MaterialTheme.colorScheme.surfaceVariant.copy(alpha = if (enabled) 1f else 0.55f)
    val thumbStrokeColor = MaterialTheme.colorScheme.surface
    val density = LocalDensity.current
    val thumbRadiusPx = with(density) { 9.dp.toPx() }
    val trackWidthPx = with(density) { 5.dp.toPx() }

    fun snap(raw: Float): Float {
        val min = valueRange.start
        val max = valueRange.endInclusive
        val snapped = (((raw - min) / stepSize).roundToInt() * stepSize) + min
        return snapped.coerceIn(min, max)
    }

    fun positionToValue(x: Float, width: Float): Float {
        val left = thumbRadiusPx
        val right = (width - thumbRadiusPx).coerceAtLeast(left + 1f)
        val fraction = ((x.coerceIn(left, right) - left) / (right - left)).coerceIn(0f, 1f)
        return snap(valueRange.start + fraction * (valueRange.endInclusive - valueRange.start))
    }

    Canvas(
        modifier = modifier
            .height(34.dp)
            .pointerInput(enabled, valueRange, stepSize) {
                if (!enabled) return@pointerInput
                detectTapGestures { offset ->
                    onValueChange(positionToValue(offset.x, size.width.toFloat()))
                }
            }
            .pointerInput(enabled, valueRange, stepSize) {
                if (!enabled) return@pointerInput
                detectDragGestures { change, _ ->
                    onValueChange(positionToValue(change.position.x, size.width.toFloat()))
                }
            }
    ) {
        val centerY = size.height / 2f
        val left = thumbRadiusPx
        val right = size.width - thumbRadiusPx
        val range = (valueRange.endInclusive - valueRange.start).coerceAtLeast(1f)
        val fraction = ((value - valueRange.start) / range).coerceIn(0f, 1f)
        val thumbX = left + (right - left) * fraction

        drawLine(
            color = inactiveColor,
            start = Offset(left, centerY),
            end = Offset(right, centerY),
            strokeWidth = trackWidthPx,
            cap = StrokeCap.Round
        )
        drawLine(
            color = activeColor,
            start = Offset(left, centerY),
            end = Offset(thumbX, centerY),
            strokeWidth = trackWidthPx,
            cap = StrokeCap.Round
        )

        val tickCount = (((valueRange.endInclusive - valueRange.start) / stepSize).roundToInt()).coerceAtLeast(1)
        repeat(tickCount + 1) { index ->
            val tickFraction = index / tickCount.toFloat()
            val tickX = left + (right - left) * tickFraction
            drawCircle(
                color = if (tickX <= thumbX) activeColor else inactiveColor,
                radius = 2.dp.toPx(),
                center = Offset(tickX, centerY)
            )
        }

        drawCircle(
            color = activeColor,
            radius = thumbRadiusPx,
            center = Offset(thumbX, centerY)
        )
        drawCircle(
            color = thumbStrokeColor,
            radius = thumbRadiusPx,
            center = Offset(thumbX, centerY),
            style = androidx.compose.ui.graphics.drawscope.Stroke(width = 2.dp.toPx())
        )
    }
}

// ═══ Important Info Dialog ═══
@Composable
fun ImportantInfoDialog(onDismiss: () -> Unit) {
    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false)
    ) {
        Surface(
            modifier = Modifier.fillMaxWidth(0.95f).padding(8.dp),
            shape = RoundedCornerShape(24.dp),
            color = MaterialTheme.colorScheme.surface,
            contentColor = MaterialTheme.colorScheme.onSurface,
            tonalElevation = 6.dp,
        ) {
            Column(modifier = Modifier.padding(24.dp).verticalScroll(rememberScrollState())) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text("Важная информация", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
                    IconButton(onClick = onDismiss) {
                        Icon(Icons.Default.Close, null)
                    }
                }

                Spacer(Modifier.height(16.dp))

                InfoSection("Капча ВК",
                    "По умолчанию в приложении установлен ручной режим (WBV + РУЧ), но его можно заменить на RJS-АВТ. Это продвинутый автоматический метод решения капчи без всплывающих окон и участия человека, основанный на реверс-инжиниринге JS-кода капчи. Он имитирует действия пользователя в фоновом режиме, обеспечивая бесперебойную работу.\n\nВАЖНО: Если в вашем случае RJS не проходит капчу или выдает ошибки (проблемы со связью или изменения на стороне ВК) — переключитесь обратно в ручной режим."
                )
                InfoSection("Как решать капчу",
                    "Она не сложная: нужно просто потянуть слайдер вправо так, чтобы все элементы (обычно это 3 слова) идеально сошлись в пазле."
                )
                InfoSection("Сетевое окружение",
                    "Отключите другие VPN/Прокси и «Приватный DNS» перед использованием."
                )
                InfoSection("Связь потоков и капч",
                    "Рекомендую выбирать 12-36 потока для меньшего количества капч. Если вам всё равно на частоту ввода капчи в фоне — ставьте 48 и более ради скорости."
                )

                Spacer(Modifier.height(20.dp))
                Button(
                    onClick = onDismiss,
                    modifier = Modifier.fillMaxWidth(),
                    shape = RoundedCornerShape(16.dp),
                    colors = ButtonDefaults.buttonColors(contentColor = MaterialTheme.colorScheme.onPrimary)
                ) {
                    Text("Понятно")
                }
            }
        }
    }
}

@Composable
private fun InfoSection(title: String, body: String) {
    Spacer(Modifier.height(12.dp))
    Text(
        title,
        style = MaterialTheme.typography.titleMedium,
        color = MaterialTheme.colorScheme.primary,
        fontWeight = FontWeight.Bold
    )
    Spacer(Modifier.height(4.dp))
    Text(body, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurface)
    Spacer(Modifier.height(4.dp))
}

// Округление до ближайшего кратного WORKERS_PER_GROUP
private fun roundToGroup(value: Float, maxW: Float = 96f): Float {
    val rounded = (Math.round(value / WORKERS_PER_GROUP) * WORKERS_PER_GROUP).toFloat()
    return rounded.coerceIn(WORKERS_PER_GROUP.toFloat(), maxW)
}

// ═══ Модальное окно хешей ═══
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HashesDialog(
    hash1: String,
    hash2: String,
    hash3: String,
    hash4: String,
    onSave: (String, String, String, String) -> Unit,
    onDismiss: () -> Unit
) {
    var h1 by remember { mutableStateOf(hash1) }
    var h2 by remember { mutableStateOf(hash2) }
    var h3 by remember { mutableStateOf(hash3) }
    var h4 by remember { mutableStateOf(hash4) }

    Dialog(onDismissRequest = onDismiss) {
        Surface(
            shape = RoundedCornerShape(24.dp),
            color = MaterialTheme.colorScheme.surface,
            contentColor = MaterialTheme.colorScheme.onSurface,
            tonalElevation = 8.dp
        ) {
            Column(
                modifier = Modifier.padding(24.dp).fillMaxWidth(),
                verticalArrangement = Arrangement.spacedBy(12.dp)
            ) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Icon(Icons.Default.Tag, null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(24.dp))
                        Spacer(Modifier.width(8.dp))
                        Text("VK Хеши", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
                    }
                    IconButton(onClick = onDismiss) {
                        Icon(Icons.Default.Close, contentDescription = "Закрыть")
                    }
                }

                Text(
                    text = "Больше хешей — выше лимит потоков и лучшее распределение нагрузки.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(bottom = 4.dp)
                )

                listOf(
                    Triple("VK Хеш 1 *", h1) { v: String -> h1 = v },
                    Triple("VK Хеш 2", h2) { v: String -> h2 = v },
                    Triple("VK Хеш 3", h3) { v: String -> h3 = v },
                    Triple("VK Хеш 4", h4) { v: String -> h4 = v }
                ).forEachIndexed { idx, (label, value, onChange) ->
                    val isShort = value.isNotBlank() && value.length < 16
                    OutlinedTextField(
                        value = value,
                        onValueChange = { raw ->
                            val cleaned = raw.filter { c -> c != ' ' && c != '\n' }
                            onChange(stripVkUrlStatic(cleaned))
                        },
                        label = { Text(label) },
                        placeholder = { Text("Ссылка звонка или хеш") },
                        singleLine = true,
                        isError = isShort,
                        supportingText = if (isShort) {
                            { Text("Хеш ${idx + 1} — короткий (мин. 16)", color = MaterialTheme.colorScheme.error) }
                        } else null,
                        modifier = Modifier.fillMaxWidth(),
                        shape = RoundedCornerShape(16.dp),
                    )
                }

                Button(
                    onClick = {
                        onSave(h1, h2, h3, h4)
                    },
                    modifier = Modifier.fillMaxWidth().height(48.dp),
                    shape = RoundedCornerShape(16.dp),
                    enabled = h1.isNotBlank() && h1.length >= 16,
                    colors = ButtonDefaults.buttonColors(contentColor = MaterialTheme.colorScheme.onPrimary)
                ) {
                    Text("Сохранить", fontWeight = FontWeight.SemiBold)
                }
            }
        }
    }
}

// ═══ Модальное окно секретов ═══
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SecretsDialog(
    settingsStore: SettingsStore,
    initialPassword: String,
    manualPortsEnabled: Boolean,
    initialServerDtlsPort: String,
    initialServerWgPort: String,
    initialLocalPort: String,
    onSaved: (String, String, String) -> Unit,
    onDismiss: () -> Unit
) {
    val scope = rememberCoroutineScope()
    var passwordInput by rememberSaveable { mutableStateOf(initialPassword) }
    var serverDtlsPort by rememberSaveable { mutableStateOf(initialServerDtlsPort.ifBlank { "56000" }) }
    var serverWgPort by rememberSaveable { mutableStateOf(initialServerWgPort.ifBlank { "56001" }) }
    var localPort by rememberSaveable { mutableStateOf(initialLocalPort.ifBlank { "9000" }) }

    fun normalizePort(value: String, fallback: String): String {
        return value.toIntOrNull()?.takeIf { it in 1..65535 }?.toString() ?: fallback
    }

    Dialog(onDismissRequest = onDismiss) {
        Surface(
            shape = RoundedCornerShape(24.dp),
            color = MaterialTheme.colorScheme.surface,
            contentColor = MaterialTheme.colorScheme.onSurface,
            tonalElevation = 8.dp
        ) {
            Column(
                modifier = Modifier.padding(24.dp).fillMaxWidth().verticalScroll(rememberScrollState())
            ) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Icon(
                            imageVector = Icons.Default.Key,
                            contentDescription = null,
                            tint = MaterialTheme.colorScheme.primary,
                            modifier = Modifier.size(24.dp)
                        )
                        Spacer(modifier = Modifier.width(8.dp))
                        Text("Секреты", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
                    }
                    IconButton(onClick = onDismiss) {
                        Icon(imageVector = Icons.Default.Close, contentDescription = "Закрыть")
                    }
                }

                Spacer(modifier = Modifier.height(16.dp))

                OutlinedTextField(
                    value = passwordInput,
                    onValueChange = { passwordInput = it },
                    label = { Text("Заданный пароль туннеля") },
                    placeholder = { Text("Придумайте надежный пароль") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                    shape = RoundedCornerShape(16.dp),
                )

                if (manualPortsEnabled) {
                    Spacer(modifier = Modifier.height(16.dp))
                    HorizontalDivider()
                    Spacer(modifier = Modifier.height(8.dp))
                    Text("Порты", color = MaterialTheme.colorScheme.primary, fontWeight = FontWeight.SemiBold)
                    Spacer(modifier = Modifier.height(8.dp))
                    OutlinedTextField(
                        value = serverDtlsPort,
                        onValueChange = { serverDtlsPort = it.filter(Char::isDigit).take(5) },
                        label = { Text("Порт сервера DTLS") },
                        placeholder = { Text("56000") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                        shape = RoundedCornerShape(16.dp),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number)
                    )
                    Spacer(modifier = Modifier.height(8.dp))
                    OutlinedTextField(
                        value = serverWgPort,
                        onValueChange = { serverWgPort = it.filter(Char::isDigit).take(5) },
                        label = { Text("Порт сервера WireGuard") },
                        placeholder = { Text("56001") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                        shape = RoundedCornerShape(16.dp),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number)
                    )
                    Spacer(modifier = Modifier.height(8.dp))
                    OutlinedTextField(
                        value = localPort,
                        onValueChange = { localPort = it.filter(Char::isDigit).take(5) },
                        label = { Text("Локальный порт VPN") },
                        placeholder = { Text("9000") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                        shape = RoundedCornerShape(16.dp),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number)
                    )
                }

                Spacer(modifier = Modifier.height(20.dp))

                Button(
                    onClick = {
                        val finalDtls = normalizePort(serverDtlsPort, "56000")
                        val finalWg = normalizePort(serverWgPort, "56001")
                        val finalLocal = normalizePort(localPort, "9000")
                        scope.launch {
                            settingsStore.saveConnectionPassword(passwordInput)
                            settingsStore.savePorts(finalDtls.toInt(), finalWg.toInt(), finalLocal.toInt())
                            onSaved(finalDtls, finalWg, finalLocal)
                            onDismiss()
                        }
                    },
                    modifier = Modifier.fillMaxWidth().height(48.dp),
                    shape = RoundedCornerShape(16.dp),
                    enabled = passwordInput.isNotEmpty(),
                    colors = ButtonDefaults.buttonColors(contentColor = MaterialTheme.colorScheme.onPrimary)
                ) {
                    Text("Сохранить", fontWeight = FontWeight.SemiBold)
                }
            }
        }
    }
}

// extension
private fun androidx.compose.ui.graphics.Color.luminance(): Float {
    val r = red
    val g = green
    val b = blue
    return 0.2126f * r + 0.7152f * g + 0.0722f * b
}

@Composable
private fun SpeedGraphCard(speedHistory: List<Float>, currentSpeed: Float) {
    val colors = MaterialTheme.colorScheme
    val isDark = colors.background.luminance() < 0.22f
    val cardBg = if (isDark) colors.surface.copy(alpha = 0.4f) else Color.White.copy(alpha = 0.5f)
    val cardBorder = colors.outlineVariant.copy(alpha = if (isDark) 0.35f else 0.2f)

    Surface(
        shape = RoundedCornerShape(16.dp),
        color = cardBg,
        border = BorderStroke(1.dp, cardBorder),
        modifier = Modifier.fillMaxWidth()
    ) {
        Column(
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp)
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp)
                ) {
                    Text(
                        text = "Скорость:",
                        style = MaterialTheme.typography.bodySmall,
                        color = colors.onSurfaceVariant
                    )
                    Text(
                        text = formatSpeed(currentSpeed),
                        style = MaterialTheme.typography.bodyMedium,
                        fontWeight = FontWeight.Bold,
                        color = colors.primary
                    )
                }

                Surface(
                    shape = RoundedCornerShape(8.dp),
                    color = colors.primaryContainer.copy(alpha = 0.4f)
                ) {
                    Row(
                        modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(4.dp)
                      ) {
                          Box(
                              modifier = Modifier
                                  .size(6.dp)
                                  .clip(androidx.compose.foundation.shape.CircleShape)
                                  .background(colors.primary)
                          )
                          Text(
                              text = "LIVE",
                              style = MaterialTheme.typography.labelSmall.copy(fontSize = 8.sp),
                              fontWeight = FontWeight.Bold,
                              color = colors.primary
                          )
                      }
                  }
              }

              Box(
                  modifier = Modifier
                      .fillMaxWidth()
                      .height(44.dp)
              ) {
                Canvas(modifier = Modifier.fillMaxSize()) {
                    val width = size.width
                    val height = size.height
                    
                    if (speedHistory.size > 1) {
                        val maxVal = speedHistory.maxOrNull()?.coerceAtLeast(10f) ?: 10f
                        val stepX = width / (speedHistory.size - 1)
                        
                        val path = Path()
                        path.moveTo(0f, height - (speedHistory[0] / maxVal) * height)
                        
                        for (i in 1 until speedHistory.size) {
                            val x = i * stepX
                            val y = height - (speedHistory[i] / maxVal) * height
                            val prevX = (i - 1) * stepX
                            val prevY = height - (speedHistory[i - 1] / maxVal) * height
                            
                            val cx1 = prevX + stepX / 2f
                            val cy1 = prevY
                            val cx2 = prevX + stepX / 2f
                            val cy2 = y
                            
                            path.cubicTo(cx1, cy1, cx2, cy2, x, y)
                        }
                        
                        val fillPath = Path().apply {
                            addPath(path)
                            lineTo(width, height)
                            lineTo(0f, height)
                            close()
                        }
                        
                        drawPath(
                            path = fillPath,
                            brush = Brush.verticalGradient(
                                colors = listOf(
                                    colors.primary.copy(alpha = 0.24f),
                                    Color.Transparent
                                )
                            )
                        )
                        
                        drawPath(
                            path = path,
                            color = colors.primary,
                            style = Stroke(
                                width = 2.5.dp.toPx(),
                                cap = StrokeCap.Round
                            )
                        )
                        
                        val lastY = height - (speedHistory.last() / maxVal) * height
                        drawCircle(
                            color = colors.primary,
                            radius = 4.5.dp.toPx(),
                            center = Offset(width, lastY)
                        )
                        drawCircle(
                            color = colors.primary.copy(alpha = 0.35f),
                            radius = 9.dp.toPx(),
                            center = Offset(width, lastY)
                        )
                    }
                }
            }
        }
    }
}

private fun formatSpeed(kbps: Float): String {
    return when {
        kbps >= 1024f -> String.format("%.2f МБ/с", kbps / 1024f)
        else -> String.format("%.1f КБ/с", kbps)
    }
}

private fun parseTrafficMb(stats: String): Double? {
    val match = Regex("Трафик:\\s*([\\d.,]+)").find(stats)
    return match?.groupValues?.getOrNull(1)?.replace(",", ".")?.toDoubleOrNull()
}

@Composable
private fun PaletteCircleOption(
    paletteId: String,
    colorHex: Long,
    selectedId: String,
    onClick: (String) -> Unit
) {
    val isSelected = paletteId == selectedId
    val baseModifier = Modifier
        .size(36.dp)
        .clip(androidx.compose.foundation.shape.CircleShape)
        .background(Color(colorHex))
        .clickable { onClick(paletteId) }

    val finalModifier = if (isSelected) {
        baseModifier.border(3.dp, MaterialTheme.colorScheme.primary, androidx.compose.foundation.shape.CircleShape)
    } else {
        baseModifier
    }

    androidx.compose.foundation.layout.Box(
        modifier = finalModifier
    )
}
