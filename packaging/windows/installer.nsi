Unicode true

!ifndef RELEASE_TAG
    !error "RELEASE_TAG is required"
!endif
!ifndef VERSION
    !error "VERSION is required"
!endif
!ifndef FIXED_VERSION
    !error "FIXED_VERSION is required"
!endif
!ifndef PACKAGE_DIR
    !error "PACKAGE_DIR is required"
!endif
!ifndef INSTALLER_STATE_HELPER
    !error "INSTALLER_STATE_HELPER is required"
!endif
!ifndef QUIET_UNINSTALL_HELPER
    !error "QUIET_UNINSTALL_HELPER is required"
!endif
!ifndef INSTALLER_DEFINITION
    !error "INSTALLER_DEFINITION is required"
!endif
!ifndef OUTPUT_FILE
    !error "OUTPUT_FILE is required"
!endif
!ifndef OUTPUT_FILE_NAME
    !error "OUTPUT_FILE_NAME is required"
!endif
!ifndef APP_NAME
    !error "APP_NAME is required"
!endif
!ifndef APP_DISPLAY_NAME
    !error "APP_DISPLAY_NAME is required"
!endif
!ifndef APP_EXECUTABLE
    !error "APP_EXECUTABLE is required"
!endif
!ifndef APP_BASE_SCRIPT
    !error "APP_BASE_SCRIPT is required"
!endif
!ifndef APP_STACK_SCRIPT
    !error "APP_STACK_SCRIPT is required"
!endif
!ifndef APP_LICENSE
    !error "APP_LICENSE is required"
!endif
!ifndef APP_CONFIG_FILE
    !error "APP_CONFIG_FILE is required"
!endif
!ifndef APP_USER_SCRIPT
    !error "APP_USER_SCRIPT is required"
!endif
!ifndef APP_PROJECT_DIRECTORY
    !error "APP_PROJECT_DIRECTORY is required"
!endif
!ifndef APP_INSTALL_DIRECTORY
    !error "APP_INSTALL_DIRECTORY is required"
!endif
!ifndef APP_PUBLISHER
    !error "APP_PUBLISHER is required"
!endif
!ifndef APP_PRODUCT_URL
    !error "APP_PRODUCT_URL is required"
!endif
!ifndef APP_PRODUCT_GUID
    !error "APP_PRODUCT_GUID is required"
!endif
!ifndef APP_UNINSTALL_KEY
    !error "APP_UNINSTALL_KEY is required"
!endif
!ifndef APP_INSTALLER_MARKER
    !error "APP_INSTALLER_MARKER is required"
!endif
!ifndef APP_QUIET_UNINSTALL_HELPER
    !error "APP_QUIET_UNINSTALL_HELPER is required"
!endif
!ifndef APP_COPYRIGHT
    !error "APP_COPYRIGHT is required"
!endif

!if "${RELEASE_TAG}" == ""
    !error "RELEASE_TAG must not be empty"
!endif
!if "${VERSION}" == ""
    !error "VERSION must not be empty"
!endif
!if "${FIXED_VERSION}" == ""
    !error "FIXED_VERSION must not be empty"
!endif
!if "${APP_PRODUCT_GUID}" == ""
    !error "APP_PRODUCT_GUID must not be empty"
!endif
!if "${APP_UNINSTALL_KEY}" == ""
    !error "APP_UNINSTALL_KEY must not be empty"
!endif

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "nsDialogs.nsh"
!include "WinMessages.nsh"
!include "WinVer.nsh"
!include "x64.nsh"

!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_UNINSTALL_KEY}"
!define APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS 100
!define APP_LIFECYCLE_MUTEX_NAME "Global\${APP_PRODUCT_GUID}.InstallerLifecycle.v2"
!define APP_ERROR_ALREADY_EXISTS 183
!define APP_EXIT_INVALID_ARGUMENTS 30
!define APP_EXIT_LIFECYCLE_BUSY 41
!define APP_EXIT_UNSUPPORTED_PLATFORM 50
!define APP_EXIT_PAYLOAD_FAILURE 60
!define APP_EXIT_INSTALL_FAILED 70
!define APP_EXIT_UNINSTALL_PREFLIGHT 80
!define APP_EXIT_UNINSTALL_FINALIZE 81
!define APP_EXIT_INTERNAL_STATE 90

Var DeleteConfigurationOnUninstall
Var DeleteConfigurationCheckbox
Var InstallerLifecycleMutexHandle
Var EnvironmentNotificationFailed

Name "${APP_DISPLAY_NAME}"
OutFile "${OUTPUT_FILE}"
InstallDir "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"
RequestExecutionLevel user
CRCCheck force
SetCompressor lzma
SetDatablockOptimize on
SetCompressorDictSize 8
SetCompressor /SOLID /FINAL lzma
AllowSkipFiles off
ManifestDPIAware true
ShowInstDetails show
AutoCloseWindow true

VIProductVersion "${FIXED_VERSION}"
VIFileVersion "${FIXED_VERSION}"
VIAddVersionKey "ProductName" "${APP_DISPLAY_NAME}"
VIAddVersionKey "FileDescription" "${APP_DISPLAY_NAME} Installer"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "CompanyName" "${APP_PUBLISHER}"
VIAddVersionKey "LegalCopyright" "${APP_COPYRIGHT}"
VIAddVersionKey "OriginalFilename" "${OUTPUT_FILE_NAME}"

!define MUI_ABORTWARNING
!define INSTALLER_WELCOME_BITMAP_100 "${__FILEDIR__}\assets\installer-welcome-finish-164x314.bmp"
!define INSTALLER_WELCOME_BITMAP_125 "${__FILEDIR__}\assets\installer-welcome-finish-205x393.bmp"
!define INSTALLER_WELCOME_BITMAP_150 "${__FILEDIR__}\assets\installer-welcome-finish-246x471.bmp"
!define INSTALLER_WELCOME_BITMAP_175 "${__FILEDIR__}\assets\installer-welcome-finish-287x550.bmp"
!define INSTALLER_WELCOME_BITMAP_200 "${__FILEDIR__}\assets\installer-welcome-finish-328x628.bmp"
!define MUI_WELCOMEFINISHPAGE_BITMAP "${INSTALLER_WELCOME_BITMAP_100}"
!define MUI_WELCOMEFINISHPAGE_BITMAP_STRETCH NoStretchNoCropNoAlign
!define MUI_CUSTOMFUNCTION_GUIINIT SelectInstallerWelcomeBitmap
!pragma verifyloadimage "${INSTALLER_WELCOME_BITMAP_125}"
!pragma verifyloadimage "${INSTALLER_WELCOME_BITMAP_150}"
!pragma verifyloadimage "${INSTALLER_WELCOME_BITMAP_175}"
!pragma verifyloadimage "${INSTALLER_WELCOME_BITMAP_200}"
!define MUI_WELCOMEPAGE_TITLE "Install ${APP_DISPLAY_NAME} ${VERSION}"
!define MUI_WELCOMEPAGE_TEXT "This setup installs ${APP_DISPLAY_NAME} for your Windows account and creates its default configuration when missing.$\r$\n$\r$\nNo administrator access is required. Open a new terminal after setup so it can find ${APP_NAME} on PATH."
!define MUI_FINISHPAGE_NOREBOOTSUPPORT
!define MUI_FINISHPAGE_TITLE "${APP_DISPLAY_NAME} ${VERSION} is installed"
!define MUI_FINISHPAGE_TEXT "Setup completed successfully.$\r$\n$\r$\n${APP_DISPLAY_NAME} is a command-line tool, so no application window opens.$\r$\n$\r$\nOpen a new terminal and go to a project directory.$\r$\n$\r$\nFor a new project, run:$\r$\n${APP_NAME} init$\r$\n$\r$\nFor an existing profile, run:$\r$\n${APP_NAME} up$\r$\n$\r$\nTo edit settings, run:$\r$\n${APP_NAME} config"
!define MUI_FINISHPAGE_LINK "Open setup and usage guide"
!define MUI_FINISHPAGE_LINK_LOCATION "${APP_PRODUCT_URL}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${PACKAGE_DIR}\${APP_LICENSE}"
!insertmacro MUI_PAGE_INSTFILES
!define MUI_PAGE_CUSTOMFUNCTION_SHOW PositionInstallerFinishLink
!insertmacro MUI_PAGE_FINISH
UninstPage custom un.DeleteConfigurationPage un.DeleteConfigurationPageLeave
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Function SelectInstallerWelcomeBitmap
    ; Resolve dynamically so unsupported hosts fail to the 96-DPI asset rather
    ; than failing GUI initialization before the Windows-version message.
    System::Call 'KERNEL32::GetModuleHandleW(w "USER32.DLL") p.r0'
    System::Call 'KERNEL32::GetProcAddress(p r0, m "GetDpiForWindow") p.r1'
    ${If} $1 == 0
        StrCpy $0 96
    ${Else}
        System::Call '::$1(p $HWNDPARENT)i.r0'
        ${If} $0 == 0
            StrCpy $0 96
        ${EndIf}
    ${EndIf}
    ${If} $0 >= 180
        File "/oname=$PLUGINSDIR\modern-wizard.bmp" "${INSTALLER_WELCOME_BITMAP_200}"
    ${ElseIf} $0 >= 156
        File "/oname=$PLUGINSDIR\modern-wizard.bmp" "${INSTALLER_WELCOME_BITMAP_175}"
    ${ElseIf} $0 >= 132
        File "/oname=$PLUGINSDIR\modern-wizard.bmp" "${INSTALLER_WELCOME_BITMAP_150}"
    ${ElseIf} $0 >= 108
        File "/oname=$PLUGINSDIR\modern-wizard.bmp" "${INSTALLER_WELCOME_BITMAP_125}"
    ${EndIf}
FunctionEnd

Function PositionInstallerFinishLink
    System::Store "S"
    ${NSD_GetText} $mui.FinishPage.Text $0
    System::Call 'USER32::GetWindowRect(p $mui.FinishPage.Text, @r1)'
    System::Call '*$1(i.r2, i.r3, i.r4, i.r5)'
    IntOp $4 $4 - $2
    System::Call '*$1(i 0, i 0, i r4, i 0)'
    System::Call 'USER32::GetDC(p $mui.FinishPage.Text) p.r6'
    SendMessage $mui.FinishPage.Text ${WM_GETFONT} 0 0 $7
    System::Call 'GDI32::SelectObject(p r6, p r7) p.s'
    System::Call 'USER32::DrawTextW(p r6, w r0, i -1, p r1, i 0x00000C10)'
    System::Call '*$1(i, i, i, i.r8)'
    System::Call 'GDI32::SelectObject(p r6, p s)'
    System::Call 'USER32::ReleaseDC(p $mui.FinishPage.Text, p r6)'

    System::Call 'USER32::GetWindowRect(p $mui.FinishPage.Text, @r1)'
    System::Call 'USER32::MapWindowPoints(p 0, p $mui.FinishPage, p r1, i 2)'
    System::Call '*$1(i.r2, i.r3, i.r4, i.r5)'
    IntOp $7 $4 - $2
    System::Call 'USER32::SetWindowPos(p $mui.FinishPage.Text, p 0, i r2, i r3, i r7, i r8, i 0x14)'

    System::Call 'USER32::GetWindowRect(p $mui.FinishPage.Link, @r1)'
    System::Call 'USER32::MapWindowPoints(p 0, p $mui.FinishPage, p r1, i 2)'
    System::Call '*$1(i.r2, i.r4, i.r5, i.r6)'
    IntOp $7 $5 - $2
    IntOp $9 $6 - $4
    IntOp $8 $8 + $3
    IntOp $8 $8 + $9
    System::Call 'USER32::SetWindowPos(p $mui.FinishPage.Link, p 0, i r2, i r8, i r7, i r9, i 0x14)'
    System::Store "L"
FunctionEnd

!macro AcquireInstallerLifecycleMutex
    ; Existence, rather than mutex ownership, is the process-lifetime gate. The
    ; non-inheritable handle closes automatically on normal exit or a hard crash.
    System::Call 'KERNEL32::SetLastError(i 0)'
    System::Call 'KERNEL32::CreateMutexW(p 0, i 0, w "${APP_LIFECYCLE_MUTEX_NAME}") p.r1 ?e'
    Pop $0
    StrCpy $InstallerLifecycleMutexHandle $1
    ${If} $1 == 0
        MessageBox MB_ICONSTOP|MB_OK "Windows could not create the ${APP_DISPLAY_NAME} installer lifecycle gate. No files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_INTERNAL_STATE}
        Quit
    ${ElseIf} $0 == ${APP_ERROR_ALREADY_EXISTS}
        System::Call 'KERNEL32::CloseHandle(p $1)'
        StrCpy $InstallerLifecycleMutexHandle 0
        MessageBox MB_ICONEXCLAMATION|MB_OK "Another ${APP_DISPLAY_NAME} setup or uninstall is already running. Wait for it to finish, then try again." /SD IDOK
        SetErrorLevel ${APP_EXIT_LIFECYCLE_BUSY}
        Quit
    ${EndIf}
!macroend

Function .onInit
    ${IfNot} ${AtLeastWin10}
        MessageBox MB_ICONSTOP|MB_OK "${APP_DISPLAY_NAME} requires Windows 10 or later." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNSUPPORTED_PLATFORM}
        Quit
    ${EndIf}
    ${IfNot} ${RunningX64}
        MessageBox MB_ICONSTOP|MB_OK "${APP_DISPLAY_NAME} requires 64-bit Windows." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNSUPPORTED_PLATFORM}
        Quit
    ${EndIf}
    !insertmacro AcquireInstallerLifecycleMutex
    SetRegView 64
    SetShellVarContext current
    StrCpy $INSTDIR "$LOCALAPPDATA\Programs\${APP_INSTALL_DIRECTORY}"
    StrCpy $EnvironmentNotificationFailed "0"
FunctionEnd

Function un.onInit
    ${IfNot} ${AtLeastWin10}
        MessageBox MB_ICONSTOP|MB_OK "${APP_DISPLAY_NAME} requires Windows 10 or later." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNSUPPORTED_PLATFORM}
        Quit
    ${EndIf}
    ${IfNot} ${RunningX64}
        MessageBox MB_ICONSTOP|MB_OK "${APP_DISPLAY_NAME} requires 64-bit Windows." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNSUPPORTED_PLATFORM}
        Quit
    ${EndIf}
    !insertmacro AcquireInstallerLifecycleMutex
    SetRegView 64
    SetShellVarContext current
    StrCpy $DeleteConfigurationOnUninstall "0"
    ${GetParameters} $0
    StrCmp $0 "" done
    StrCmp $0 "/S" done
    StrCmp $0 "/DELETE_CONFIG" delete_config
    StrCmp $0 "/S /DELETE_CONFIG" delete_config
    StrCmp $0 "/DELETE_CONFIG /S" delete_config
    MessageBox MB_ICONSTOP|MB_OK "Unsupported uninstall arguments. Use only /S and the exact /DELETE_CONFIG option." /SD IDOK
    SetErrorLevel ${APP_EXIT_INVALID_ARGUMENTS}
    Quit
    delete_config:
        StrCpy $DeleteConfigurationOnUninstall "1"
    done:
FunctionEnd

Function un.DeleteConfigurationPage
    IfSilent done 0
    nsDialogs::Create 1018
    Pop $0
    ${If} $0 == error
        Abort
    ${EndIf}

    ${NSD_CreateLabel} 0 0 100% 72u "Uninstall removes ${APP_DISPLAY_NAME} machine-local state, SSH integration, and the configured package/tool cache. A running Sandbox stays open but becomes unmanaged; close it manually when finished. Select this option to also remove ${APP_CONFIG_FILE} and ${APP_USER_SCRIPT}. Project ${APP_PROJECT_DIRECTORY} profiles are not removed."
    Pop $0
    ${NSD_CreateCheckbox} 0 82u 100% 14u "Also delete ${APP_CONFIG_FILE} and ${APP_USER_SCRIPT}"
    Pop $DeleteConfigurationCheckbox
    ${If} $DeleteConfigurationOnUninstall == "1"
        ${NSD_Check} $DeleteConfigurationCheckbox
    ${EndIf}
    nsDialogs::Show
    done:
FunctionEnd

Function un.DeleteConfigurationPageLeave
    IfSilent done 0
    ${If} $DeleteConfigurationCheckbox == ""
        Goto done
    ${EndIf}
    ${NSD_GetState} $DeleteConfigurationCheckbox $0
    ${If} $0 == ${BST_CHECKED}
        StrCpy $DeleteConfigurationOnUninstall "1"
    ${Else}
        StrCpy $DeleteConfigurationOnUninstall "0"
    ${EndIf}
    done:
FunctionEnd

!macro ExtractInstallerControlFiles
    InitPluginsDir
    SetOutPath "$PLUGINSDIR\control"
    File "/oname=installer-state.ps1" "${INSTALLER_STATE_HELPER}"
    File "/oname=definition.json" "${INSTALLER_DEFINITION}"
!macroend

!macro RunInstallerState ACTION EXTRA
    SetOutPath "$PLUGINSDIR\control"
    nsExec::ExecToStack /TIMEOUT=120000 '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$PLUGINSDIR\control\installer-state.ps1" -Action ${ACTION} -DefinitionPath "$PLUGINSDIR\control\definition.json" -InstallDirectory "$INSTDIR" ${EXTRA}'
    Pop $0
    Pop $1
!macroend

Section "Install"
    DetailPrint "Validating and activating ${APP_DISPLAY_NAME} ${VERSION}..."
    InitPluginsDir
    SetOutPath "$PLUGINSDIR\package"
    ClearErrors
    File "${PACKAGE_DIR}\${APP_BASE_SCRIPT}"
    File "${PACKAGE_DIR}\${APP_LICENSE}"
    File "${PACKAGE_DIR}\${APP_STACK_SCRIPT}"
    File "/oname=${APP_QUIET_UNINSTALL_HELPER}" "${QUIET_UNINSTALL_HELPER}"
    File "${PACKAGE_DIR}\${APP_EXECUTABLE}"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not extract the ${APP_DISPLAY_NAME} application package." /SD IDOK
        SetErrorLevel ${APP_EXIT_PAYLOAD_FAILURE}
        Quit
    ${EndIf}
    ClearErrors
    WriteUninstaller "$PLUGINSDIR\package\uninstall.exe"
    ${If} ${Errors}
        StrCpy $R4 "Could not create the ${APP_DISPLAY_NAME} uninstaller."
        MessageBox MB_ICONSTOP|MB_OK "$R4 No installed state was changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_PAYLOAD_FAILURE}
        Quit
    ${EndIf}

    !insertmacro ExtractInstallerControlFiles
    !insertmacro RunInstallerState "Install" '-PackageDirectory "$PLUGINSDIR\package"'
    StrCpy $R9 $0
    ${If} $0 != "0"
    ${AndIf} $0 != "10"
        MessageBox MB_ICONSTOP|MB_OK "Could not install ${APP_DISPLAY_NAME}: status $0. $1 Automatic transaction recovery and direct repair both failed. Close the process using any exact path named above, then run setup again." /SD IDOK
        SetErrorLevel ${APP_EXIT_INSTALL_FAILED}
        Quit
    ${EndIf}

    DetailPrint "Creating the default user configuration when missing..."
    SetOutPath "$INSTDIR"
    nsExec::ExecToStack /TIMEOUT=15000 '"$INSTDIR\${APP_EXECUTABLE}" __installer-seed-configuration'
    Pop $0
    Pop $1
    ${If} $0 != "0"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} is installed, but default configuration could not be created yet: status $0. $1 The first command that needs configuration will try again." /SD IDOK
    ${EndIf}

    ${If} $R9 == "10"
        DetailPrint "Notifying Windows about the PATH change..."
        System::Call 'KERNEL32::SetLastError(i 0)'
        System::Call 'USER32::SendMessageTimeoutW(p ${HWND_BROADCAST}, i ${WM_SETTINGCHANGE}, p 0, w "Environment", i 0x2, i ${APP_ENVIRONMENT_BROADCAST_TIMEOUT_MS}, *p .r3) p.r2 ?e'
        Pop $4
        ${If} $2 == 0
            StrCpy $EnvironmentNotificationFailed "1"
            DetailPrint "Windows did not confirm the PATH-change notification (error $4)."
        ${EndIf}
    ${EndIf}
    ${If} $EnvironmentNotificationFailed == "1"
        MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} is installed, but Windows did not confirm the PATH refresh. Open a new terminal. If it still cannot find ${APP_NAME}, sign out and back in." /SD IDOK
    ${EndIf}
SectionEnd

Section "Uninstall"
    SetAutoClose true
    !insertmacro ExtractInstallerControlFiles
    DetailPrint "Validating ${APP_DISPLAY_NAME} ownership and deletion readiness..."
    !insertmacro RunInstallerState "InspectUninstall" ""
    StrCpy $R9 $0
    ${If} $0 != "0"
    ${AndIf} $0 != "10"
        MessageBox MB_ICONSTOP|MB_OK "Could not safely begin ${APP_DISPLAY_NAME} uninstall: status $0. $1 No application state or files were changed." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_PREFLIGHT}
        Quit
    ${EndIf}

    ${If} $R9 == "0"
        DetailPrint "Removing ${APP_DISPLAY_NAME} state, SSH integration, and cache; any running Sandbox stays open..."
        SetOutPath "$INSTDIR"
        ${If} $DeleteConfigurationOnUninstall == "1"
            nsExec::ExecToStack /TIMEOUT=960000 '"$INSTDIR\${APP_EXECUTABLE}" __installer-clean-uninstall --installer-schema=1 --delete-configuration'
        ${Else}
            nsExec::ExecToStack /TIMEOUT=960000 '"$INSTDIR\${APP_EXECUTABLE}" __installer-clean-uninstall --installer-schema=1'
        ${EndIf}
        Pop $0
        Pop $1
        ${If} $0 != "0"
            MessageBox MB_ICONEXCLAMATION|MB_OK "${APP_DISPLAY_NAME} application files will still be removed, but some machine-local state could not be cleaned: status $0. $1 Locked or unsafe residual state was preserved." /SD IDOK
        ${Else}
            !insertmacro RunInstallerState "MarkCleanupComplete" ""
            ${If} $0 != "0"
                DetailPrint "Durable uninstall recording failed; terminal removal will converge directly: status $0. $1"
            ${EndIf}
        ${EndIf}
    ${Else}
        DetailPrint "Resuming ${APP_DISPLAY_NAME} after completed application-state cleanup."
    ${EndIf}

    !insertmacro RunInstallerState "FinishUninstall" ""
    ${If} $0 != "0"
        MessageBox MB_ICONSTOP|MB_OK "Could not finish ${APP_DISPLAY_NAME} uninstall: status $0. $1 Automatic transaction recovery and direct removal both failed. Close the process using any exact path named above, then run uninstall again." /SD IDOK
        SetErrorLevel ${APP_EXIT_UNINSTALL_FINALIZE}
        Quit
    ${EndIf}
SectionEnd
