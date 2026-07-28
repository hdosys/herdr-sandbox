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
!ifndef PATH_HELPER
    !error "PATH_HELPER is required"
!endif
!ifndef OUTPUT_FILE
    !error "OUTPUT_FILE is required"
!endif

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "WinMessages.nsh"
!include "x64.nsh"

!define PRODUCT_NAME "Herdr Sandbox"
!define PRODUCT_EXE "herdr-sandbox.exe"
!define PRODUCT_URL "https://github.com/hdosys/herdr-sandbox"
!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\HerdrSandbox"

Name "${PRODUCT_NAME}"
OutFile "${OUTPUT_FILE}"
InstallDir "$LOCALAPPDATA\Programs\Herdr Sandbox"
RequestExecutionLevel user
SetCompressor /SOLID lzma
AllowSkipFiles off
ManifestDPIAware true

VIProductVersion "${FIXED_VERSION}"
VIFileVersion "${FIXED_VERSION}"
VIAddVersionKey "ProductName" "${PRODUCT_NAME}"
VIAddVersionKey "FileDescription" "${PRODUCT_NAME} Installer"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "CompanyName" "hdosys"
VIAddVersionKey "LegalCopyright" "Copyright (c) 2026 hdosys"
VIAddVersionKey "OriginalFilename" "herdr-sandbox_${RELEASE_TAG}_windows_amd64_setup.exe"

!define MUI_ABORTWARNING
!define MUI_WELCOMEPAGE_TITLE "Install ${PRODUCT_NAME} ${VERSION}"
!define MUI_WELCOMEPAGE_TEXT "This installs the Herdr Sandbox command-line tool for your Windows account.$\r$\n$\r$\nNo administrator access is required. Open a new terminal after setup so it can find herdr-sandbox on PATH."
!define MUI_FINISHPAGE_TITLE "${PRODUCT_NAME} is installed"
!define MUI_FINISHPAGE_TEXT "Open a new terminal and run herdr-sandbox --help. Herdr and Windows prerequisites are not installed by this setup."

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Function .onInit
    ${IfNot} ${RunningX64}
        MessageBox MB_ICONSTOP|MB_OK "${PRODUCT_NAME} requires 64-bit Windows." /SD IDOK
        Abort
    ${EndIf}
    SetRegView 64
    SetShellVarContext current
    StrCpy $INSTDIR "$LOCALAPPDATA\Programs\Herdr Sandbox"
FunctionEnd

Function un.onInit
    ${IfNot} ${RunningX64}
        MessageBox MB_ICONSTOP|MB_OK "${PRODUCT_NAME} requires 64-bit Windows." /SD IDOK
        Abort
    ${EndIf}
    SetRegView 64
    SetShellVarContext current
    StrCpy $INSTDIR "$LOCALAPPDATA\Programs\Herdr Sandbox"
FunctionEnd

!macro UpdateUserPath ACTION
    InitPluginsDir
    SetOutPath "$PLUGINSDIR"
    File "/oname=path.ps1" "${PATH_HELPER}"
    System::Call 'Kernel32::SetEnvironmentVariable(t "HERDR_SANDBOX_INSTALL_DIRECTORY", t "$INSTDIR")i.r3'
    ${If} $3 == "0"
        MessageBox MB_ICONSTOP|MB_OK "Could not prepare the current-user PATH update." /SD IDOK
        Abort
    ${EndIf}
    ClearErrors
    nsExec::ExecToStack /TIMEOUT=15000 '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$PLUGINSDIR\path.ps1" -Action ${ACTION}'
    Pop $0
    Pop $1
    System::Call 'Kernel32::SetEnvironmentVariable(t "HERDR_SANDBOX_INSTALL_DIRECTORY", t n)i.r3'
    ${If} $0 == "0"
    ${ElseIf} $0 == "10"
    ${Else}
        DetailPrint "Could not update the current-user PATH: $1"
        SetErrors
        Abort
    ${EndIf}
    ClearErrors
    SendMessage ${HWND_BROADCAST} ${WM_WININICHANGE} 0 "STR:Environment" /TIMEOUT=5000
    ${If} ${Errors}
        MessageBox MB_ICONEXCLAMATION|MB_OK "The user PATH was saved, but Windows did not acknowledge the change. Sign out or restart Windows before using ${PRODUCT_NAME} from a terminal." /SD IDOK
        ClearErrors
    ${EndIf}
!macroend

!macro BackupRuntimeFile NAME FLAG
    StrCpy ${FLAG} "0"
    ${If} ${FileExists} "$INSTDIR\${NAME}"
        ClearErrors
        CopyFiles /SILENT "$INSTDIR\${NAME}" "$PLUGINSDIR\backup"
        ${If} ${Errors}
            MessageBox MB_ICONSTOP|MB_OK "Could not back up the installed ${NAME}. Close running commands and try again." /SD IDOK
            Abort
        ${EndIf}
        StrCpy ${FLAG} "1"
    ${EndIf}
!macroend

!macro ReplaceRuntimeFile NAME
    ${If} $9 == "0"
        ClearErrors
        CopyFiles /SILENT "$PLUGINSDIR\package\${NAME}" "$INSTDIR"
        ${If} ${Errors}
            StrCpy $9 "1"
        ${EndIf}
    ${EndIf}
!macroend

!macro RestoreRuntimeFile NAME FLAG
    ${If} ${FLAG} == "1"
        ClearErrors
        CopyFiles /SILENT "$PLUGINSDIR\backup\${NAME}" "$INSTDIR"
    ${Else}
        ClearErrors
        Delete "$INSTDIR\${NAME}"
    ${EndIf}
    ${If} ${Errors}
        StrCpy $R0 "1"
    ${EndIf}
!macroend

Section "Install"
    ClearErrors
    ReadRegStr $3 HKCU "${UNINSTALL_KEY}" "DisplayName"
    ${If} ${Errors}
        ClearErrors
        StrCpy $2 "0"
    ${ElseIf} $3 != "${PRODUCT_NAME}"
        MessageBox MB_ICONSTOP|MB_OK "The existing installer registration is not owned by ${PRODUCT_NAME}. No files were changed." /SD IDOK
        Abort
    ${Else}
        ClearErrors
        ReadRegDWORD $2 HKCU "${UNINSTALL_KEY}" "PathAdded"
        ${If} ${Errors}
            StrCpy $2 "0"
        ${EndIf}
    ${EndIf}

    InitPluginsDir
    SetOutPath "$PLUGINSDIR\package"
    ClearErrors
    File "${PACKAGE_DIR}\base.ps1"
    File "${PACKAGE_DIR}\herdr-sandbox.exe"
    File "${PACKAGE_DIR}\stacks.ps1"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not extract the ${PRODUCT_NAME} application package." /SD IDOK
        Abort
    ${EndIf}
    CreateDirectory "$PLUGINSDIR\backup"
    CreateDirectory "$INSTDIR"
    !insertmacro BackupRuntimeFile "base.ps1" $6
    !insertmacro BackupRuntimeFile "herdr-sandbox.exe" $7
    !insertmacro BackupRuntimeFile "stacks.ps1" $8

    StrCpy $9 "0"
    !insertmacro ReplaceRuntimeFile "herdr-sandbox.exe"
    !insertmacro ReplaceRuntimeFile "base.ps1"
    !insertmacro ReplaceRuntimeFile "stacks.ps1"
    ${If} $9 != "0"
        StrCpy $R0 "0"
        !insertmacro RestoreRuntimeFile "herdr-sandbox.exe" $7
        !insertmacro RestoreRuntimeFile "base.ps1" $6
        !insertmacro RestoreRuntimeFile "stacks.ps1" $8
        ${If} $R0 == "0"
            MessageBox MB_ICONSTOP|MB_OK "Could not update ${PRODUCT_NAME}; the prior application files were restored. Close running commands and try again." /SD IDOK
        ${Else}
            MessageBox MB_ICONSTOP|MB_OK "Could not update ${PRODUCT_NAME}, and rollback was incomplete. Close running commands and run setup again." /SD IDOK
        ${EndIf}
        Abort
    ${EndIf}

    SetOutPath "$INSTDIR"
    ClearErrors
    WriteUninstaller "$INSTDIR\uninstall.exe"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not create the ${PRODUCT_NAME} uninstaller." /SD IDOK
        Abort
    ${EndIf}

    ClearErrors
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "${PRODUCT_NAME}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "hdosys"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" '"$INSTDIR\${PRODUCT_EXE}",0'
    WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "URLInfoAbout" "${PRODUCT_URL}"
    WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
    WriteRegStr HKCU "${UNINSTALL_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not register ${PRODUCT_NAME} in Windows Installed Apps." /SD IDOK
        Abort
    ${EndIf}

    !insertmacro UpdateUserPath "Add"
    StrCpy $4 $0
    ${If} $2 == "1"
        StrCpy $5 "1"
    ${ElseIf} $4 == "10"
        StrCpy $5 "1"
    ${Else}
        StrCpy $5 "0"
    ${EndIf}
    ClearErrors
    WriteRegDWORD HKCU "${UNINSTALL_KEY}" "PathAdded" $5
    ${If} ${Errors}
        ${If} $4 == "10"
            !insertmacro UpdateUserPath "Remove"
        ${EndIf}
        MessageBox MB_ICONSTOP|MB_OK "Could not record ${PRODUCT_NAME} PATH ownership." /SD IDOK
        Abort
    ${EndIf}
SectionEnd

Section "Uninstall"
    SetOutPath "$TEMP"
    ClearErrors
    ReadRegStr $3 HKCU "${UNINSTALL_KEY}" "DisplayName"
    ${If} ${Errors}
        ClearErrors
        StrCpy $2 "0"
        StrCpy $4 "0"
    ${ElseIf} $3 != "${PRODUCT_NAME}"
        MessageBox MB_ICONSTOP|MB_OK "The installer registration is not owned by ${PRODUCT_NAME}. No files were changed." /SD IDOK
        Abort
    ${Else}
        StrCpy $4 "1"
        ClearErrors
        ReadRegDWORD $2 HKCU "${UNINSTALL_KEY}" "PathAdded"
        ${If} ${Errors}
            StrCpy $2 "0"
        ${EndIf}
    ${EndIf}

    ClearErrors
    Delete "$INSTDIR\herdr-sandbox.exe"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Close any running ${PRODUCT_NAME} command and try uninstalling again. No running process was terminated." /SD IDOK
        Abort
    ${EndIf}
    ClearErrors
    Delete "$INSTDIR\base.ps1"
    Delete "$INSTDIR\stacks.ps1"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not remove the installed ${PRODUCT_NAME} files. Check their permissions and try again." /SD IDOK
        Abort
    ${EndIf}
    ${If} $2 == "1"
        !insertmacro UpdateUserPath "Remove"
    ${EndIf}
    ${If} $4 == "1"
        ClearErrors
        DeleteRegKey HKCU "${UNINSTALL_KEY}"
        ${If} ${Errors}
            MessageBox MB_ICONSTOP|MB_OK "Could not remove ${PRODUCT_NAME} from Windows Installed Apps." /SD IDOK
            Abort
        ${EndIf}
    ${EndIf}
    ClearErrors
    Delete "$INSTDIR\uninstall.exe"
    ${If} ${Errors}
        MessageBox MB_ICONSTOP|MB_OK "Could not remove the ${PRODUCT_NAME} uninstaller." /SD IDOK
        Abort
    ${EndIf}
    ClearErrors
    RMDir "$INSTDIR"
SectionEnd
