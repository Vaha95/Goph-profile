(function () {
    "use strict";

    var parts = window.location.pathname.split("/");
    var userId = decodeURIComponent(parts[parts.length - 1]);

    var grid = document.getElementById("galleryGrid");
    var empty = document.getElementById("galleryEmpty");
    var info = document.getElementById("galleryInfo");
    var errorBanner = document.getElementById("errorBanner");

    function showError(msg) {
        errorBanner.textContent = msg;
        errorBanner.style.display = "block";
    }

    function formatDate(d) {
        var date = new Date(d);
        return date.toLocaleDateString() + " " + date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    }

    function formatStatus(s) {
        var labels = {
            "complete": "Done",
            "pending": "Processing",
            "in_progress": "Processing",
            "failed": "Failed"
        };
        return labels[s] || s;
    }

    function fetchAvatars() {
        var xhr = new XMLHttpRequest();
        xhr.open("GET", "/api/v1/users/" + encodeURIComponent(userId) + "/avatars");
        xhr.setRequestHeader("Content-Type", "application/json");

        xhr.onload = function () {
            grid.innerHTML = "";
            if (xhr.status === 200) {
                var avatars = JSON.parse(xhr.responseText);
                if (avatars.length === 0) {
                    empty.style.display = "block";
                    info.textContent = "User: " + userId;
                    return;
                }
                info.textContent = avatars.length + " avatar" + (avatars.length > 1 ? "s" : "") + " for " + userId;

                avatars.forEach(function (a) {
                    var card = document.createElement("div");
                    card.className = "avatar-card";

                    var img = document.createElement("img");
                    img.src = "/api/v1/avatars/" + a.id + "?size=300x300";
                    img.alt = "Avatar";

                    var meta = document.createElement("div");
                    meta.className = "card-meta";

                    var status = document.createElement("span");
                    status.className = "status status-" + a.status;
                    status.textContent = formatStatus(a.status);

                    var date = document.createElement("div");
                    date.textContent = formatDate(a.created_at);

                    meta.appendChild(status);
                    meta.appendChild(document.createElement("br"));
                    meta.appendChild(date);
                    card.appendChild(img);
                    card.appendChild(meta);
                    grid.appendChild(card);
                });
            } else if (xhr.status === 404) {
                showError("No avatars found for user " + userId);
                empty.style.display = "block";
                info.textContent = "User: " + userId;
            } else {
                showError("Failed to load avatars");
            }
        };

        xhr.onerror = function () {
            grid.innerHTML = "";
            showError("Network error. Please try again.");
        };

        xhr.send();
    }

    if (userId) {
        fetchAvatars();
    } else {
        empty.style.display = "block";
        grid.innerHTML = "";
    }

})();
