package com.wdtt.client

import android.content.Context
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.emptyPreferences
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withContext
import kotlinx.coroutines.flow.first
import java.util.UUID

data class ConnectionProfile(
    val id: String,
    val name: String,
    val peer: String,
    val vkHashes: String,
    val workersPerHash: Int,
    val listenPort: Int,
    val password: String,
    val trafficMb: Double = 0.0,
    val groupId: String = "",
    val useGlobalHashes: Boolean = true
)

data class ProfileGroup(
    val id: String,
    val name: String
)

class ProfilesStore(context: Context) {
    private val appContext = context.applicationContext
    private val settings = SettingsStore(appContext)

    companion object {
        private val Context.dataStore by preferencesDataStore("profiles")
        private const val IDS_KEY = "profiles_ids"
        private fun idsKey() = stringPreferencesKey(IDS_KEY)

        private fun nameKey(id: String) = stringPreferencesKey("profile_name_$id")
        private fun peerKey(id: String) = stringPreferencesKey("profile_peer_$id")
        private fun hashesKey(id: String) = stringPreferencesKey("profile_hashes_$id")
        private fun workersKey(id: String) = intPreferencesKey("profile_workers_$id")
        private fun portKey(id: String) = intPreferencesKey("profile_port_$id")
        private fun passKey(id: String) = stringPreferencesKey("profile_pass_enc_$id")
        private fun useGlobalHashesKey(id: String) = booleanPreferencesKey("profile_use_global_hashes_$id")
        private fun trafficKey(id: String) = androidx.datastore.preferences.core.doublePreferencesKey("profile_traffic_$id")
        private fun groupIdKey(id: String) = stringPreferencesKey("profile_group_id_$id")

        private const val GROUPS_IDS_KEY = "groups_ids"
        private fun groupsIdsKey() = stringPreferencesKey(GROUPS_IDS_KEY)
        private fun groupNameKey(id: String) = stringPreferencesKey("group_name_$id")
    }

    private val dataStore = appContext.dataStore
    private val secureStore = SecureStringStore(appContext)

    val profiles: Flow<List<ConnectionProfile>> = dataStore.data
        .catch { emit(emptyPreferences()) }
        .map { prefs ->
            val idsRaw = prefs[idsKey()] ?: ""
            if (idsRaw.isBlank()) return@map emptyList()
            val ids = idsRaw.split(',').map { it.trim() }.filter { it.isNotEmpty() }
            val list = mutableListOf<ConnectionProfile>()
            for (id in ids) {
                val name = prefs[nameKey(id)] ?: ""
                val peer = prefs[peerKey(id)] ?: ""
                val hashes = prefs[hashesKey(id)] ?: ""
                val workers = prefs[workersKey(id)] ?: 16
                val port = prefs[portKey(id)] ?: 9000
                val enc = prefs[passKey(id)] ?: ""
                val pass = secureStore.decrypt(enc) ?: ""
                val traffic = prefs[trafficKey(id)] ?: 0.0
                val groupId = prefs[groupIdKey(id)] ?: ""
                val useGlobal = prefs[useGlobalHashesKey(id)] ?: true
                list.add(ConnectionProfile(id, name, peer, hashes, workers, port, pass, traffic, groupId, useGlobal))
            }
            list
        }

    val groups: Flow<List<ProfileGroup>> = dataStore.data
        .catch { emit(emptyPreferences()) }
        .map { prefs ->
            val idsRaw = prefs[groupsIdsKey()] ?: ""
            if (idsRaw.isBlank()) return@map emptyList()
            val ids = idsRaw.split(',').map { it.trim() }.filter { it.isNotEmpty() }
            val list = mutableListOf<ProfileGroup>()
            for (id in ids) {
                val name = prefs[groupNameKey(id)] ?: ""
                list.add(ProfileGroup(id, name))
            }
            list
        }

    suspend fun saveProfile(profile: ConnectionProfile) = withContext(Dispatchers.IO) {
        dataStore.edit { prefs ->
            val idsRaw = prefs[idsKey()] ?: ""
            val ids = idsRaw.split(',').map { it.trim() }.filter { it.isNotEmpty() }.toMutableList()
            if (!ids.contains(profile.id)) ids.add(profile.id)
            prefs[idsKey()] = ids.joinToString(",")

            prefs[nameKey(profile.id)] = profile.name
            prefs[peerKey(profile.id)] = profile.peer
            prefs[hashesKey(profile.id)] = profile.vkHashes
            prefs[workersKey(profile.id)] = profile.workersPerHash
            prefs[portKey(profile.id)] = profile.listenPort
            prefs[passKey(profile.id)] = secureStore.encrypt(profile.password)
            prefs[trafficKey(profile.id)] = profile.trafficMb
            prefs[groupIdKey(profile.id)] = profile.groupId
            prefs[useGlobalHashesKey(profile.id)] = profile.useGlobalHashes
        }
    }

    suspend fun saveGroup(group: ProfileGroup) = withContext(Dispatchers.IO) {
        dataStore.edit { prefs ->
            val idsRaw = prefs[groupsIdsKey()] ?: ""
            val ids = idsRaw.split(',').map { it.trim() }.filter { it.isNotEmpty() }.toMutableList()
            if (!ids.contains(group.id)) ids.add(group.id)
            prefs[groupsIdsKey()] = ids.joinToString(",")
            prefs[groupNameKey(group.id)] = group.name
        }
    }

    suspend fun deleteProfile(id: String) = withContext(Dispatchers.IO) {
        dataStore.edit { prefs ->
            val idsRaw = prefs[idsKey()] ?: ""
            val ids = idsRaw.split(',').map { it.trim() }.filter { it.isNotEmpty() }.toMutableList()
            ids.remove(id)
            prefs[idsKey()] = ids.joinToString(",")
            prefs.remove(nameKey(id))
            prefs.remove(peerKey(id))
            prefs.remove(hashesKey(id))
            prefs.remove(workersKey(id))
            prefs.remove(portKey(id))
            prefs.remove(passKey(id))
            prefs.remove(useGlobalHashesKey(id))
            prefs.remove(trafficKey(id))
            prefs.remove(groupIdKey(id))
        }
    }

    suspend fun deleteGroup(id: String) = withContext(Dispatchers.IO) {
        dataStore.edit { prefs ->
            val idsRaw = prefs[groupsIdsKey()] ?: ""
            val ids = idsRaw.split(',').map { it.trim() }.filter { it.isNotEmpty() }.toMutableList()
            ids.remove(id)
            prefs[groupsIdsKey()] = ids.joinToString(",")
            prefs.remove(groupNameKey(id))
        }
    }

    suspend fun createProfile(name: String, peer: String, vkHashes: String, workers: Int, listenPort: Int, password: String, groupId: String = ""): ConnectionProfile {
        val id = UUID.randomUUID().toString()
        val p = ConnectionProfile(id, name, peer, vkHashes, workers, listenPort, password, 0.0, groupId)
        saveProfile(p)
        return p
    }

    suspend fun getProfileOnce(id: String): ConnectionProfile? {
        val prefs = dataStore.data.first()
        val name = prefs[nameKey(id)] ?: return null
        val peer = prefs[peerKey(id)] ?: ""
        val hashes = prefs[hashesKey(id)] ?: ""
        val workers = prefs[workersKey(id)] ?: 16
        val port = prefs[portKey(id)] ?: 9000
        val enc = prefs[passKey(id)] ?: ""
        val pass = secureStore.decrypt(enc) ?: ""
        val traffic = prefs[trafficKey(id)] ?: 0.0
        val groupId = prefs[groupIdKey(id)] ?: ""
        return ConnectionProfile(id, name, peer, hashes, workers, port, pass, traffic, groupId)
    }

    suspend fun incrementProfileTraffic(id: String, additionalTrafficMb: Double) = withContext(Dispatchers.IO) {
        if (id.isBlank() || additionalTrafficMb <= 0.0) return@withContext
        dataStore.edit { prefs ->
            val key = trafficKey(id)
            val current = prefs[key] ?: 0.0
            prefs[key] = current + additionalTrafficMb
        }
    }

    suspend fun resetProfileTraffic(id: String) = withContext(Dispatchers.IO) {
        dataStore.edit { prefs ->
            prefs[trafficKey(id)] = 0.0
        }
    }

    suspend fun reorderProfiles(newOrderForSubset: List<String>) = withContext(Dispatchers.IO) {
        dataStore.edit { prefs ->
            val actualIdsRaw = prefs[stringPreferencesKey("profiles_ids")] ?: ""
            val actualIds = actualIdsRaw.split(",").map { it.trim() }.filter { it.isNotEmpty() }.toMutableList()
            
            val subsetIndices = actualIds.mapIndexedNotNull { index, id -> if (newOrderForSubset.contains(id)) index else null }.sorted()
            if (subsetIndices.size == newOrderForSubset.size) {
                for (i in subsetIndices.indices) {
                    actualIds[subsetIndices[i]] = newOrderForSubset[i]
                }
                prefs[stringPreferencesKey("profiles_ids")] = actualIds.joinToString(",")
            } else if (actualIds.isEmpty() || newOrderForSubset.containsAll(actualIds)) {
                prefs[stringPreferencesKey("profiles_ids")] = newOrderForSubset.joinToString(",")
            }
        }
    }

    // Apply profile: save to SettingsStore and optionally start tunnel
    suspend fun applyProfile(context: Context, id: String, startImmediately: Boolean = false) {
        val p = getProfileOnce(id) ?: return
        // save to settings
        val finalHashes = if (p.useGlobalHashes) {
            val global = settings.vkHashes.first()
            global.ifEmpty { p.vkHashes }
        } else {
            p.vkHashes
        }
        settings.save(p.peer, finalHashes, "", p.workersPerHash, "udp", p.listenPort)
        settings.saveConnectionPassword(p.password)
        settings.saveCurrentProfile(p.id, p.name)
        if (startImmediately) {
            val captchaMode = settings.captchaMode.first()
            val captchaSolve = settings.captchaSolveMethod.first()
            val detailedLogs = settings.detailedLogs.first()
            val params = TunnelParams(p.peer, finalHashes, "", p.workersPerHash, p.listenPort, "", p.password, "udp", captchaMode, captchaSolve, detailedLogs)
            TunnelManager.start(context, params)
        }
    }
}
