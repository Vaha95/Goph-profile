(function () {
    "use strict";

    var dropZone = document.getElementById("dropZone");
    var fileInput = document.getElementById("fileInput");
    var userIdInput = document.getElementById("userIdInput");
    var preview = document.getElementById("preview");
    var previewImage = document.getElementById("previewImage");
    var previewInfo = document.getElementById("previewInfo");
    var clearBtn = document.getElementById("clearBtn");
    var uploadBtn = document.getElementById("uploadBtn");
    var progress = document.getElementById("progress");
    var errorBanner = document.getElementById("errorBanner");

    var selectedFile = null;
    var maxSize = 10 * 1024 * 1024;

    function showError(msg) {
        errorBanner.textContent = msg;
        errorBanner.style.display = "block";
        setTimeout(function () { errorBanner.style.display = "none"; }, 5000);
    }

    function formatSize(bytes) {
        if (bytes < 1024) return bytes + " B";
        if (bytes < 1048576) return (bytes / 1024).toFixed(1) + " KB";
        return (bytes / 1048576).toFixed(1) + " MB";
    }

    // Show redirect error from server
    var params = new URLSearchParams(window.location.search);
    if (params.get("error")) {
        showError("Upload failed. Please try again.");
    }

    function setFile(file) {
        if (!file) return;

        var allowed = ["image/jpeg", "image/png", "image/webp"];
        if (!allowed.includes(file.type)) {
            showError("Invalid format. Supported: JPEG, PNG, WebP.");
            return;
        }

        if (file.size > maxSize) {
            showError("File too large. Maximum size is 10 MB.");
            return;
        }

        selectedFile = file;
        var reader = new FileReader();
        reader.onload = function (e) {
            previewImage.src = e.target.result;
            previewInfo.textContent = file.name + " — " + formatSize(file.size);
            preview.style.display = "block";
            dropZone.style.display = "none";
            uploadBtn.disabled = false;
        };
        reader.readAsDataURL(file);
    }

    function clearFile() {
        selectedFile = null;
        fileInput.value = "";
        preview.style.display = "none";
        previewImage.src = "";
        previewInfo.textContent = "";
        dropZone.style.display = "";
        uploadBtn.disabled = true;
    }

    function upload() {
        var userId = userIdInput.value.trim();
        if (!userId) { showError("Please enter a User ID."); return; }
        if (!selectedFile) { showError("Please select a file."); return; }

        uploadBtn.disabled = true;
        uploadBtn.textContent = "Uploading...";
        progress.style.display = "block";
        errorBanner.style.display = "none";

        var formData = new FormData();
        formData.append("file", selectedFile);

        var xhr = new XMLHttpRequest();
        xhr.open("POST", "/api/v1/avatars");
        xhr.setRequestHeader("X-User-ID", userId);

        xhr.upload.onprogress = function (e) {
            if (e.lengthComputable) {
                var pct = (e.loaded / e.total) * 100;
                var bar = document.getElementById("progressBar");
                bar.style.width = pct + "%";
                bar.style.animation = "none";
            }
        };

        xhr.onload = function () {
            progress.style.display = "none";
            if (xhr.status === 201) {
                window.location.href = "/web/gallery/" + encodeURIComponent(userId);
            } else {
                var body = JSON.parse(xhr.responseText || "{}");
                showError(body.error || "Upload failed");
                uploadBtn.disabled = false;
                uploadBtn.textContent = "Upload";
            }
        };

        xhr.onerror = function () {
            progress.style.display = "none";
            showError("Network error. Please try again.");
            uploadBtn.disabled = false;
            uploadBtn.textContent = "Upload";
        };

        xhr.send(formData);
    }

    // Drag & drop
    dropZone.addEventListener("dragover", function (e) {
        e.preventDefault();
        dropZone.classList.add("dragover");
    });
    dropZone.addEventListener("dragleave", function () {
        dropZone.classList.remove("dragover");
    });
    dropZone.addEventListener("drop", function (e) {
        e.preventDefault();
        dropZone.classList.remove("dragover");
        if (e.dataTransfer.files.length) setFile(e.dataTransfer.files[0]);
    });

    // File input
    fileInput.addEventListener("change", function () {
        if (fileInput.files.length) setFile(fileInput.files[0]);
    });

    // Buttons
    clearBtn.addEventListener("click", clearFile);
    uploadBtn.addEventListener("click", upload);

})();
