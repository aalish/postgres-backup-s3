// Multi-Database UI Enhancement JavaScript

// Global state for multi-database support
let multiDbConfig = {
    enabled: false,
    databases: [],
    selectedDatabase: null
};

// Initialize multi-database support
async function initMultiDatabase() {
    try {
        const response = await fetch('/api/multidb/databases', {
            credentials: 'same-origin'
        });

        if (!response.ok) {
            console.warn('Multi-database API not available');
            return;
        }

        const data = await response.json();
        multiDbConfig.enabled = data.multidb_enabled;
        multiDbConfig.databases = data.databases;

        if (multiDbConfig.enabled) {
            // Add database selector to UI
            addDatabaseSelector();
            // Load database-specific stats
            loadDatabaseStats();
            // Update UI to show multi-database features
            enhanceUIForMultiDatabase();
        }
    } catch (error) {
        console.error('Failed to initialize multi-database support:', error);
    }
}

// Add database selector to the page header
function addDatabaseSelector() {
    // Find the main header or navigation area
    const header = document.querySelector('.page-header, .navbar, header');
    if (!header) return;

    // Create database selector container
    const selectorContainer = document.createElement('div');
    selectorContainer.className = 'database-selector-container';
    selectorContainer.innerHTML = `
        <style>
            .database-selector-container {
                position: fixed;
                top: 10px;
                right: 20px;
                z-index: 1000;
                background: white;
                border-radius: 8px;
                box-shadow: 0 2px 10px rgba(0,0,0,0.1);
                padding: 10px;
            }
            .database-selector {
                display: flex;
                align-items: center;
                gap: 10px;
            }
            .database-selector label {
                font-weight: 600;
                color: #333;
            }
            .database-selector select {
                padding: 8px 12px;
                border: 2px solid #4F46E5;
                border-radius: 6px;
                background: white;
                color: #333;
                font-size: 14px;
                min-width: 200px;
                cursor: pointer;
            }
            .database-selector select:hover {
                border-color: #6366F1;
            }
            .database-info {
                margin-top: 8px;
                padding: 8px;
                background: #F3F4F6;
                border-radius: 4px;
                font-size: 12px;
            }
            .database-status {
                display: inline-block;
                padding: 2px 8px;
                border-radius: 12px;
                font-size: 11px;
                font-weight: 600;
                margin-left: 8px;
            }
            .status-healthy {
                background: #10B981;
                color: white;
            }
            .status-warning {
                background: #F59E0B;
                color: white;
            }
            .status-error {
                background: #EF4444;
                color: white;
            }
            .database-actions {
                margin-top: 10px;
                display: flex;
                gap: 8px;
            }
            .db-action-btn {
                padding: 6px 12px;
                border-radius: 4px;
                border: none;
                font-size: 12px;
                cursor: pointer;
                transition: all 0.2s;
            }
            .db-action-btn.primary {
                background: #4F46E5;
                color: white;
            }
            .db-action-btn.primary:hover {
                background: #6366F1;
            }
            .db-action-btn.secondary {
                background: #E5E7EB;
                color: #374151;
            }
            .db-action-btn.secondary:hover {
                background: #D1D5DB;
            }
        </style>
        <div class="database-selector">
            <label>Select Database:</label>
            <select id="global-db-selector">
                <option value="">-- Choose Database --</option>
                ${multiDbConfig.databases.map(db => `
                    <option value="${db.id}" ${!db.enabled ? 'disabled' : ''}>
                        ${db.name} ${!db.enabled ? '(Disabled)' : ''}
                    </option>
                `).join('')}
            </select>
        </div>
        <div id="database-info" class="database-info" style="display: none;">
            <!-- Database details will be shown here -->
        </div>
        <div id="database-actions" class="database-actions" style="display: none;">
            <button class="db-action-btn primary" onclick="backupSelectedDatabase()">
                Backup This Database
            </button>
            <button class="db-action-btn secondary" onclick="viewDatabaseBackups()">
                View Backups
            </button>
        </div>
    `;

    document.body.appendChild(selectorContainer);

    // Add event listener for database selection
    document.getElementById('global-db-selector').addEventListener('change', onDatabaseSelected);

    // Load saved selection
    const savedSelection = localStorage.getItem('selected_database');
    if (savedSelection && multiDbConfig.databases.find(db => db.id === savedSelection)) {
        document.getElementById('global-db-selector').value = savedSelection;
        onDatabaseSelected();
    }
}

// Handle database selection
function onDatabaseSelected() {
    const selector = document.getElementById('global-db-selector');
    const selectedId = selector.value;

    if (!selectedId) {
        document.getElementById('database-info').style.display = 'none';
        document.getElementById('database-actions').style.display = 'none';
        multiDbConfig.selectedDatabase = null;
        return;
    }

    const database = multiDbConfig.databases.find(db => db.id === selectedId);
    if (!database) return;

    multiDbConfig.selectedDatabase = database;
    localStorage.setItem('selected_database', selectedId);

    // Show database info
    const infoDiv = document.getElementById('database-info');
    infoDiv.innerHTML = `
        <div><strong>${database.name}</strong></div>
        <div>Host: ${database.host}:${database.port}</div>
        <div>Database: ${database.database}</div>
        <div>User: ${database.user}</div>
        <div>Schedule: ${database.backup_schedule || 'Not scheduled'}</div>
        <div>Retention: ${database.retention_days} days</div>
        <div>Status: <span class="database-status status-${database.status}">${database.status.toUpperCase()}</span></div>
    `;
    infoDiv.style.display = 'block';

    // Show actions
    document.getElementById('database-actions').style.display = 'flex';

    // Refresh page content with selected database
    refreshPageForDatabase(database);
}

// Backup selected database
async function backupSelectedDatabase() {
    if (!multiDbConfig.selectedDatabase) {
        alert('Please select a database first');
        return;
    }

    const database = multiDbConfig.selectedDatabase;

    if (!confirm(`Start backup for ${database.name}?`)) {
        return;
    }

    try {
        const response = await fetch('/api/multidb/backup', {
            method: 'POST',
            credentials: 'same-origin',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': getCSRFToken()
            },
            body: JSON.stringify({
                database_id: database.id
            })
        });

        if (!response.ok) {
            throw new Error('Backup request failed');
        }

        const result = await response.json();

        // Show success message
        showNotification(`Backup started for ${database.name}`, 'success');

        // Refresh backup list
        if (typeof loadBackups === 'function') {
            loadBackups();
        }
    } catch (error) {
        console.error('Failed to trigger backup:', error);
        showNotification(`Failed to start backup: ${error.message}`, 'error');
    }
}

// View backups for selected database
function viewDatabaseBackups() {
    if (!multiDbConfig.selectedDatabase) {
        alert('Please select a database first');
        return;
    }

    // Navigate to backups page with database filter
    window.location.href = `/backups?database_id=${multiDbConfig.selectedDatabase.id}`;
}

// Load database statistics
async function loadDatabaseStats() {
    try {
        const response = await fetch('/api/multidb/stats', {
            credentials: 'same-origin'
        });

        if (!response.ok) return;

        const data = await response.json();

        // Update dashboard with database stats
        updateDashboardWithStats(data.databases);
    } catch (error) {
        console.error('Failed to load database stats:', error);
    }
}

// Update dashboard with database statistics
function updateDashboardWithStats(databases) {
    // Find or create a stats container
    let statsContainer = document.getElementById('multidb-stats');
    if (!statsContainer) {
        statsContainer = document.createElement('div');
        statsContainer.id = 'multidb-stats';
        statsContainer.className = 'multidb-stats-container';

        // Find a suitable place to insert it
        const dashboardContent = document.querySelector('.dashboard-content, .page-content, main');
        if (dashboardContent) {
            dashboardContent.insertBefore(statsContainer, dashboardContent.firstChild);
        }
    }

    statsContainer.innerHTML = `
        <style>
            .multidb-stats-container {
                margin: 20px 0;
                padding: 20px;
                background: white;
                border-radius: 8px;
                box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            }
            .multidb-stats-title {
                font-size: 18px;
                font-weight: 600;
                margin-bottom: 15px;
                color: #1F2937;
            }
            .database-grid {
                display: grid;
                grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
                gap: 15px;
            }
            .database-card {
                padding: 15px;
                border: 1px solid #E5E7EB;
                border-radius: 6px;
                background: #F9FAFB;
            }
            .database-card-header {
                display: flex;
                justify-content: space-between;
                align-items: center;
                margin-bottom: 10px;
            }
            .database-card-title {
                font-weight: 600;
                color: #374151;
            }
            .database-card-stats {
                display: grid;
                grid-template-columns: repeat(2, 1fr);
                gap: 8px;
                font-size: 13px;
            }
            .stat-item {
                display: flex;
                justify-content: space-between;
            }
            .stat-label {
                color: #6B7280;
            }
            .stat-value {
                font-weight: 600;
                color: #111827;
            }
        </style>
        <div class="multidb-stats-title">📊 Database Overview</div>
        <div class="database-grid">
            ${databases.map(db => `
                <div class="database-card">
                    <div class="database-card-header">
                        <div class="database-card-title">${db.database_name}</div>
                        <span class="database-status status-${db.status || 'healthy'}">
                            ${(db.status || 'healthy').toUpperCase()}
                        </span>
                    </div>
                    <div class="database-card-stats">
                        <div class="stat-item">
                            <span class="stat-label">Host:</span>
                            <span class="stat-value">${db.host}</span>
                        </div>
                        <div class="stat-item">
                            <span class="stat-label">Backups:</span>
                            <span class="stat-value">${db.backup_count || 0}</span>
                        </div>
                        <div class="stat-item">
                            <span class="stat-label">Success:</span>
                            <span class="stat-value">${db.success_count || 0}</span>
                        </div>
                        <div class="stat-item">
                            <span class="stat-label">Failed:</span>
                            <span class="stat-value">${db.failure_count || 0}</span>
                        </div>
                        <div class="stat-item">
                            <span class="stat-label">Last Backup:</span>
                            <span class="stat-value">${db.last_backup ? formatRelativeTime(db.last_backup) : 'Never'}</span>
                        </div>
                        <div class="stat-item">
                            <span class="stat-label">Retention:</span>
                            <span class="stat-value">${db.retention_days} days</span>
                        </div>
                    </div>
                </div>
            `).join('')}
        </div>
    `;
}

// Refresh page content for selected database
function refreshPageForDatabase(database) {
    // Update page title
    const pageTitle = document.querySelector('h1, .page-title');
    if (pageTitle && !pageTitle.hasAttribute('data-original-title')) {
        pageTitle.setAttribute('data-original-title', pageTitle.textContent);
    }
    if (pageTitle) {
        const originalTitle = pageTitle.getAttribute('data-original-title') || pageTitle.textContent;
        pageTitle.textContent = `${originalTitle} - ${database.name}`;
    }

    // If on backups page, filter by database
    if (window.location.pathname.includes('backups')) {
        if (typeof loadBackups === 'function') {
            loadBackups(database.id);
        }
    }

    // If on dashboard, load database-specific stats
    if (window.location.pathname === '/' || window.location.pathname.includes('dashboard')) {
        loadDatabaseStats();
    }
}

// Enhanced UI features for multi-database
function enhanceUIForMultiDatabase() {
    // Add visual indicators
    const body = document.body;
    body.classList.add('multidb-enabled');

    // Enhance backup trigger buttons
    const backupButtons = document.querySelectorAll('[data-action="trigger-backup"], button:contains("Backup")');
    backupButtons.forEach(button => {
        button.addEventListener('click', function(e) {
            if (!multiDbConfig.selectedDatabase) {
                e.preventDefault();
                alert('Please select a database from the dropdown first');
            }
        });
    });
}

// Helper functions
function getCSRFToken() {
    const meta = document.querySelector('meta[name="csrf-token"]');
    return meta ? meta.getAttribute('content') : '';
}

function showNotification(message, type = 'info') {
    // Create notification element
    const notification = document.createElement('div');
    notification.className = `notification notification-${type}`;
    notification.innerHTML = `
        <style>
            .notification {
                position: fixed;
                top: 80px;
                right: 20px;
                padding: 15px 20px;
                border-radius: 6px;
                box-shadow: 0 4px 6px rgba(0,0,0,0.1);
                z-index: 10000;
                animation: slideIn 0.3s ease-out;
                max-width: 400px;
            }
            .notification-success {
                background: #10B981;
                color: white;
            }
            .notification-error {
                background: #EF4444;
                color: white;
            }
            .notification-info {
                background: #3B82F6;
                color: white;
            }
            @keyframes slideIn {
                from {
                    transform: translateX(100%);
                    opacity: 0;
                }
                to {
                    transform: translateX(0);
                    opacity: 1;
                }
            }
        </style>
        ${message}
    `;

    document.body.appendChild(notification);

    // Auto-remove after 5 seconds
    setTimeout(() => {
        notification.style.animation = 'slideOut 0.3s ease-in';
        setTimeout(() => notification.remove(), 300);
    }, 5000);
}

function formatRelativeTime(dateString) {
    const date = new Date(dateString);
    const now = new Date();
    const diff = now - date;

    const minutes = Math.floor(diff / 60000);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);

    if (days > 0) return `${days} day${days > 1 ? 's' : ''} ago`;
    if (hours > 0) return `${hours} hour${hours > 1 ? 's' : ''} ago`;
    if (minutes > 0) return `${minutes} minute${minutes > 1 ? 's' : ''} ago`;
    return 'Just now';
}

// Initialize when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initMultiDatabase);
} else {
    initMultiDatabase();
}