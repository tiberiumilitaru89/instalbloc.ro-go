"use strict";

(function () {
  // Enforce zero-eval in HTMX for strict CSP compliance
  if (window.htmx && window.htmx.config) {
    window.htmx.config.allowEval = false;
    window.htmx.config.selfRequestsOnly = true;
  }

  // Register Service Worker for PWA
  if ("serviceWorker" in navigator) {
    window.addEventListener("load", function () {
      navigator.serviceWorker.register("/sw.js", { scope: "/" }).catch(function () {
        // Silent fallback in non-HTTPS local testing
      });
    });
  }

  // Register CSP-safe Alpine.js controllers
  document.addEventListener("alpine:init", function () {
    if (!window.Alpine) return;

    window.Alpine.data("mobileMenu", function () {
      return {
        open: false,
        toggle: function () {
          this.open = !this.open;
        },
        close: function () {
          this.open = false;
        }
      };
    });

    window.Alpine.data("pwaInstallBanner", function () {
      return {
        deferredPrompt: null,
        visible: false,
        init: function () {
          var self = this;
          if (window.matchMedia && window.matchMedia("(display-mode: standalone)").matches) {
            return;
          }
          window.addEventListener("beforeinstallprompt", function (e) {
            e.preventDefault();
            self.deferredPrompt = e;
            self.visible = true;
          });
        },
        install: function () {
          var self = this;
          if (!this.deferredPrompt) return;
          this.deferredPrompt.prompt();
          this.deferredPrompt.userChoice.then(function () {
            self.deferredPrompt = null;
            self.visible = false;
          });
        },
        dismiss: function () {
          this.visible = false;
        }
      };
    });
  });
})();
