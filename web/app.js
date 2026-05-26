// DOM Elements
const configDirInput = document.getElementById('config-dir');
const configSelect = document.getElementById('config-select');
const refreshConfigsBtn = document.getElementById('refresh-configs');
const accountSelect = document.getElementById('account-select');
const saveAccountBtn = document.getElementById('btn-save-account');
const deleteAccountBtn = document.getElementById('btn-delete-account');
const usernameInput = document.getElementById('username');
const passwordInput = document.getElementById('password');
const saveCredsCheckbox = document.getElementById('save-credentials');
const togglePasswordBtn = document.getElementById('toggle-password');
const btnConnect = document.getElementById('btn-connect');
const btnDisconnect = document.getElementById('btn-disconnect');

const headerStatusDot = document.getElementById('header-status-dot');
const headerStatusText = document.getElementById('header-status-text');

const stateRing = document.getElementById('state-ring');
const stateDot = document.getElementById('state-dot');
const stateName = document.getElementById('state-name');
const stateSub = document.getElementById('state-sub');

const statUptime = document.getElementById('stat-uptime');
const statIp = document.getElementById('stat-ip');
const statInterface = document.getElementById('stat-interface');

const logConsole = document.getElementById('log-console');
const clearLogsBtn = document.getElementById('btn-clear-logs');
const autoScrollCheckbox = document.getElementById('auto-scroll');
const toast = document.getElementById('toast');
const toastMessage = document.getElementById('toast-message');

// Global State Variables
let currentStatus = 'disconnected';
let uptimeInterval = null;
let uptimeSeconds = 0;
let eventSource = null;
let savedConfigs = [];
let savedAccounts = [];

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    // Load saved config directory path from localStorage
    const savedConfigDir = localStorage.getItem('config_dir') || '';
    configDirInput.value = savedConfigDir;

    fetchConfigs();
    fetchAccounts();
    fetchStatus();
    setupEventSource();
    
    // Poll status every 2 seconds
    setInterval(fetchStatus, 2000);
});

// Event Listeners
refreshConfigsBtn.addEventListener('click', fetchConfigs);
configDirInput.addEventListener('change', fetchConfigs);
configDirInput.addEventListener('keypress', (e) => {
    if (e.key === 'Enter') {
        fetchConfigs();
    }
});
togglePasswordBtn.addEventListener('click', togglePasswordVisibility);
btnConnect.addEventListener('click', connectVPN);
btnDisconnect.addEventListener('click', disconnectVPN);
clearLogsBtn.addEventListener('click', () => { logConsole.textContent = ''; });
statIp.addEventListener('click', copyIpToClipboard);
configSelect.addEventListener('change', handleConfigChange);
accountSelect.addEventListener('change', handleAccountChange);
saveAccountBtn.addEventListener('click', saveCurrentAccount);
deleteAccountBtn.addEventListener('click', deleteSelectedAccount);

// Functions
function showToast(message) {
    toastMessage.textContent = message;
    toast.classList.add('show');
    setTimeout(() => {
        toast.classList.remove('show');
    }, 3000);
}

function togglePasswordVisibility() {
    const isPassword = passwordInput.type === 'password';
    passwordInput.type = isPassword ? 'text' : 'password';
    
    togglePasswordBtn.innerHTML = isPassword ? 
        `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="eye-icon">
            <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/>
            <line x1="1" y1="1" x2="23" y2="23"/>
        </svg>` : 
        `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="eye-icon">
            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
            <circle cx="12" cy="12" r="3"/>
        </svg>`;
}

async function fetchConfigs() {
    try {
        refreshConfigsBtn.disabled = true;
        refreshConfigsBtn.style.animation = 'rotateRing 1s linear infinite';
        
        const configDir = configDirInput.value.trim();
        localStorage.setItem('config_dir', configDir);
        
        const url = configDir ? `/api/configs?custom_dir=${encodeURIComponent(configDir)}` : '/api/configs';
        const response = await fetch(url);
        if (!response.ok) {
            const errData = await response.json();
            throw new Error(errData.error || 'Failed to load configs');
        }
        
        savedConfigs = await response.json();
        
        // Save current selection if any
        const currentSelection = configSelect.value;
        
        // Clear select options except placeholder
        configSelect.innerHTML = '<option value="" disabled selected>Select a configuration file...</option>';
        
        if (savedConfigs.length === 0) {
            configSelect.innerHTML = '<option value="" disabled selected>No .ovpn files found</option>';
            showToast(configDir ? `No .ovpn files found in directory: ${configDir}` : 'No .ovpn files found in home or current directory.');
        } else {
            savedConfigs.forEach(cfg => {
                const opt = document.createElement('option');
                opt.value = cfg.path;
                opt.textContent = cfg.name;
                configSelect.appendChild(opt);
            });
            
            // Restore selection or select the first one
            if (currentSelection && savedConfigs.some(c => c.path === currentSelection)) {
                configSelect.value = currentSelection;
            } else if (savedConfigs.length > 0) {
                // Pre-select first config
                configSelect.selectedIndex = 1;
                handleConfigChange();
            }
        }
    } catch (err) {
        console.error(err);
        showToast('Error fetching OVPN configs: ' + err.message);
    } finally {
        refreshConfigsBtn.disabled = false;
        refreshConfigsBtn.style.animation = '';
    }
}

function handleConfigChange() {
    const selectedPath = configSelect.value;
    const config = savedConfigs.find(c => c.path === selectedPath);
    if (config) {
        if (config.saved_username) {
            usernameInput.value = config.saved_username;
        } else {
            usernameInput.value = '';
        }
        
        if (config.saved_password) {
            passwordInput.value = config.saved_password;
        } else {
            passwordInput.value = '';
        }
    }
}

async function fetchStatus() {
    try {
        const response = await fetch('/api/status');
        if (!response.ok) throw new Error('Status failed');
        
        const state = await response.json();
        updateUIState(state);
    } catch (err) {
        console.error(err);
        // Don't show toast for periodic status errors to avoid spamming
    }
}

function updateUIState(state) {
    const status = state.status; // disconnected, connecting, connected, disconnecting, error
    currentStatus = status;

    // Header Status Updates
    headerStatusDot.className = `status-dot ${status}`;
    headerStatusText.textContent = status.charAt(0).toUpperCase() + status.slice(1);

    // Main Ring Updates
    stateRing.className = `state-glowing-ring ${status}`;
    stateDot.className = `state-dot ${status}`;
    stateName.textContent = status.toUpperCase();
    
    // Status Subtitle Updates
    if (status === 'connected') {
        const parts = state.active_config.split('/');
        const filename = parts[parts.length - 1];
        stateSub.textContent = `Connected to ${filename}`;
    } else if (status === 'connecting') {
        stateSub.textContent = 'Establishing secure channel...';
    } else if (status === 'disconnecting') {
        stateSub.textContent = 'Closing VPN session...';
    } else if (status === 'error') {
        stateSub.textContent = state.error_msg || 'An error occurred';
    } else {
        stateSub.textContent = 'Select profile and connect';
    }

    // IP, Interface and Form elements state
    if (status === 'connected') {
        statIp.textContent = state.ip_address || 'Fetching IP...';
        if (state.ip_address) {
            statIp.classList.add('copyable');
        }
        statInterface.textContent = state.interface || 'tun0';
        
        // Form inputs and button controls
        btnConnect.disabled = true;
        btnDisconnect.disabled = false;
        configSelect.disabled = true;
        configDirInput.disabled = true;
        usernameInput.disabled = true;
        passwordInput.disabled = true;
        saveCredsCheckbox.disabled = true;
        accountSelect.disabled = true;
        saveAccountBtn.disabled = true;
        deleteAccountBtn.disabled = true;
        
        // Start uptime counter if not running
        if (!uptimeInterval && state.start_time) {
            const start = new Date(state.start_time);
            uptimeInterval = setInterval(() => {
                const diffMs = new Date() - start;
                uptimeSeconds = Math.floor(diffMs / 1000);
                if (uptimeSeconds < 0) uptimeSeconds = 0;
                statUptime.textContent = formatDuration(uptimeSeconds);
            }, 1000);
        }
    } else if (status === 'connecting') {
        statIp.textContent = '---.---.---.---';
        statIp.classList.remove('copyable');
        statInterface.textContent = 'negotiating';
        
        btnConnect.disabled = true;
        btnDisconnect.disabled = false; // Allow canceling connection
        configSelect.disabled = true;
        configDirInput.disabled = true;
        usernameInput.disabled = true;
        passwordInput.disabled = true;
        saveCredsCheckbox.disabled = true;
        accountSelect.disabled = true;
        saveAccountBtn.disabled = true;
        deleteAccountBtn.disabled = true;
        
        stopUptimeCounter();
    } else if (status === 'disconnecting') {
        btnConnect.disabled = true;
        btnDisconnect.disabled = true;
        configSelect.disabled = true;
        configDirInput.disabled = true;
        usernameInput.disabled = true;
        passwordInput.disabled = true;
        saveCredsCheckbox.disabled = true;
        accountSelect.disabled = true;
        saveAccountBtn.disabled = true;
        deleteAccountBtn.disabled = true;
    } else { // disconnected or error
        statIp.textContent = '---.---.---.---';
        statIp.classList.remove('copyable');
        statInterface.textContent = 'none';
        
        btnConnect.disabled = !configSelect.value;
        btnDisconnect.disabled = true;
        configSelect.disabled = false;
        configDirInput.disabled = false;
        usernameInput.disabled = false;
        passwordInput.disabled = false;
        saveCredsCheckbox.disabled = false;
        accountSelect.disabled = false;
        saveAccountBtn.disabled = false;
        deleteAccountBtn.disabled = false;
        
        stopUptimeCounter();
    }
}

function stopUptimeCounter() {
    if (uptimeInterval) {
        clearInterval(uptimeInterval);
        uptimeInterval = null;
    }
    statUptime.textContent = '00:00:00';
    uptimeSeconds = 0;
}

function formatDuration(seconds) {
    const hrs = Math.floor(seconds / 3600).toString().padStart(2, '0');
    const mins = Math.floor((seconds % 3600) / 60).toString().padStart(2, '0');
    const secs = (seconds % 60).toString().padStart(2, '0');
    return `${hrs}:${mins}:${secs}`;
}

function copyIpToClipboard() {
    const ip = statIp.textContent;
    if (ip && ip !== '---.---.---.---' && ip !== 'Fetching IP...') {
        navigator.clipboard.writeText(ip)
            .then(() => showToast('IP copied to clipboard: ' + ip))
            .catch(err => console.error('Could not copy text: ', err));
    }
}

async function connectVPN() {
    const configPath = configSelect.value;
    const username = usernameInput.value.trim();
    const password = passwordInput.value;
    const saveCreds = saveCredsCheckbox.checked;

    if (!configPath) {
        showToast('Please select a config profile.');
        return;
    }
    if (!username || !password) {
        showToast('Please enter username and password.');
        return;
    }

    try {
        btnConnect.disabled = true;
        const response = await fetch('/api/connect', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                config_path: configPath,
                username: username,
                password: password,
                save_credentials: saveCreds
            })
        });

        const result = await response.json();
        if (!response.ok) {
            throw new Error(result.error || 'Connection failed to start');
        }

        showToast('Initiating VPN connection...');
        logConsole.textContent = '=== Starting connection ===\n';
        fetchStatus();
    } catch (err) {
        console.error(err);
        showToast(err.message);
        btnConnect.disabled = false;
    }
}

async function disconnectVPN() {
    try {
        btnDisconnect.disabled = true;
        const response = await fetch('/api/disconnect', { method: 'POST' });
        const result = await response.json();
        if (!response.ok) {
            throw new Error(result.error || 'Disconnect failed');
        }
        showToast('Disconnecting VPN...');
        fetchStatus();
    } catch (err) {
        console.error(err);
        showToast(err.message);
        btnDisconnect.disabled = false;
    }
}

function setupEventSource() {
    if (eventSource) {
        eventSource.close();
    }

    eventSource = new EventSource('/api/logs');

    eventSource.onmessage = (event) => {
        const text = event.data;
        
        // Remove waiting text on first real log message
        if (logConsole.textContent.includes('Waiting for connection logs...')) {
            logConsole.textContent = '';
        }
        
        // Append log line
        logConsole.textContent += text + '\n';

        // Auto-scroll
        if (autoScrollCheckbox.checked) {
            logConsole.scrollTop = logConsole.scrollHeight;
        }
    };

    eventSource.onerror = (err) => {
        console.warn('Logs EventSource disconnected, retrying...', err);
        eventSource.close();
        setTimeout(setupEventSource, 3000); // Retry reconnecting
    };
}

// Saved Accounts API Logic

async function fetchAccounts() {
    try {
        const response = await fetch('/api/accounts');
        if (!response.ok) throw new Error('Failed to fetch accounts');
        savedAccounts = await response.json();
        
        // Preserve current selection if possible
        const prevSelectedVal = accountSelect.value;
        
        // Clear and rebuild options
        accountSelect.innerHTML = '<option value="">-- Enter custom credentials --</option>';
        savedAccounts.forEach(acc => {
            const option = document.createElement('option');
            option.value = acc.id;
            option.textContent = `${acc.label} (${acc.username})`;
            accountSelect.appendChild(option);
        });
        
        // Restore selection or reset inputs
        if (prevSelectedVal && savedAccounts.some(acc => acc.id === prevSelectedVal)) {
            accountSelect.value = prevSelectedVal;
            deleteAccountBtn.style.display = 'inline-flex';
        } else {
            accountSelect.value = '';
            deleteAccountBtn.style.display = 'none';
        }
    } catch (err) {
        console.error('Error fetching accounts:', err);
        showToast('Error loading saved accounts list');
    }
}

function handleAccountChange() {
    const selectedId = accountSelect.value;
    if (!selectedId) {
        // Clear inputs for custom entry
        usernameInput.value = '';
        passwordInput.value = '';
        deleteAccountBtn.style.display = 'none';
        return;
    }
    
    const account = savedAccounts.find(acc => acc.id === selectedId);
    if (account) {
        usernameInput.value = account.username;
        passwordInput.value = account.password;
        deleteAccountBtn.style.display = 'inline-flex';
    }
}

async function saveCurrentAccount() {
    const username = usernameInput.value.trim();
    const password = passwordInput.value;
    
    if (!username || !password) {
        showToast('Please enter both username and password first');
        return;
    }
    
    // Check if we are updating an existing selected account
    const selectedId = accountSelect.value;
    let label = '';
    
    if (selectedId) {
        const existing = savedAccounts.find(acc => acc.id === selectedId);
        if (existing) {
            if (confirm(`Do you want to update the password for saved account "${existing.label}"?`)) {
                label = existing.label;
            } else {
                return;
            }
        }
    }
    
    if (!label) {
        label = prompt('Enter a label/display name for this account:');
        if (!label) return; // cancelled
        label = label.trim();
        if (!label) {
            showToast('Label cannot be empty');
            return;
        }
    }
    
    try {
        const payload = {
            label: label,
            username: username,
            password: password
        };
        
        if (selectedId) {
            payload.id = selectedId;
        }
        
        const response = await fetch('/api/accounts', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        
        if (!response.ok) {
            const data = await response.json();
            throw new Error(data.error || 'Failed to save account');
        }
        
        showToast(selectedId ? 'Account updated successfully' : 'Account saved successfully');
        
        // Re-fetch accounts and select the saved one
        const updatedAccounts = await response.json();
        savedAccounts = updatedAccounts;
        
        // Find the saved account (usually the last added one if new)
        let targetId = selectedId;
        if (!targetId) {
            // Find by label & username match
            const found = updatedAccounts.find(acc => acc.label === label && acc.username === username);
            if (found) targetId = found.id;
        }
        
        // Rebuild select dropdown
        accountSelect.innerHTML = '<option value="">-- Enter custom credentials --</option>';
        updatedAccounts.forEach(acc => {
            const option = document.createElement('option');
            option.value = acc.id;
            option.textContent = `${acc.label} (${acc.username})`;
            accountSelect.appendChild(option);
        });
        
        if (targetId) {
            accountSelect.value = targetId;
            deleteAccountBtn.style.display = 'inline-flex';
        }
    } catch (err) {
        console.error('Error saving account:', err);
        showToast(err.message || 'Error saving account');
    }
}

async function deleteSelectedAccount() {
    const selectedId = accountSelect.value;
    if (!selectedId) return;
    
    const account = savedAccounts.find(acc => acc.id === selectedId);
    if (!account) return;
    
    if (!confirm(`Are you sure you want to delete the saved account "${account.label}"?`)) {
        return;
    }
    
    try {
        const response = await fetch('/api/accounts/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: selectedId })
        });
        
        if (!response.ok) {
            const data = await response.json();
            throw new Error(data.error || 'Failed to delete account');
        }
        
        showToast('Account deleted successfully');
        
        // Reset fields
        usernameInput.value = '';
        passwordInput.value = '';
        accountSelect.value = '';
        deleteAccountBtn.style.display = 'none';
        
        // Reload list
        await fetchAccounts();
    } catch (err) {
        console.error('Error deleting account:', err);
        showToast(err.message || 'Error deleting account');
    }
}
