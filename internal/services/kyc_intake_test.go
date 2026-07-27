package services

import "testing"

func hasField(fields []IntakeFieldSpec, key string) (IntakeFieldSpec, bool) {
	for _, f := range fields {
		if f.Key == key {
			return f, true
		}
	}
	return IntakeFieldSpec{}, false
}

func TestGetIntakeRequirements_Individual(t *testing.T) {
	fields := GetIntakeRequirements("individual")

	// Fields required by Nilos + AML must be present and required.
	for _, key := range []string{"legal_first_name", "legal_last_name", "date_of_birth", "nationality", "address_line1", "city", "country", "occupation", "source_of_funds", "purpose_of_account"} {
		f, ok := hasField(fields, key)
		if !ok {
			t.Fatalf("individual intake missing field %q", key)
		}
		if !f.Required {
			t.Errorf("field %q should be required for individuals", key)
		}
	}

	// BVN is collected (for the Naira account) but optional.
	bvn, ok := hasField(fields, "bvn")
	if !ok {
		t.Fatal("intake missing bvn field")
	}
	if bvn.Required {
		t.Error("bvn should be optional (Nomba BVN is optional)")
	}

	// select fields carry options.
	if sof, _ := hasField(fields, "source_of_funds"); len(sof.Options) == 0 {
		t.Error("source_of_funds should have options")
	}
}

func TestGetIntakeRequirements_BusinessRelabels(t *testing.T) {
	fields := GetIntakeRequirements("business")

	first, _ := hasField(fields, "legal_first_name")
	if first.Label != "Representative First Name" {
		t.Errorf("business first-name label = %q, want representative label", first.Label)
	}
	addr, _ := hasField(fields, "address_line1")
	if addr.Label == "" || addr.Label == "Address Line 1" {
		t.Errorf("business address label not relabeled: %q", addr.Label)
	}
	// Business adds context fields (importer + NRE counterparties/contact) on top
	// of the individual identity set.
	if len(fields) <= len(GetIntakeRequirements("individual")) {
		t.Errorf("business intake should add fields; got %d vs individual %d", len(fields), len(GetIntakeRequirements("individual")))
	}
	if _, ok := hasField(fields, "is_importer"); !ok {
		t.Error("business intake missing is_importer field")
	}
}

func TestBuildIntakeRequirements_BusinessDocuments(t *testing.T) {
	req := BuildIntakeRequirements("business", IntakeOptions{IsImporter: true, NeedsNRE: true})

	hasDoc := func(key string) (DocumentSpec, bool) {
		for _, d := range req.Documents {
			if d.Key == key {
				return d, true
			}
		}
		return DocumentSpec{}, false
	}

	// Core Nilos company documents must all be present and required.
	for _, key := range []string{
		DocCertificateOfIncorporation, DocDirectorRegister, DocShareholderRegister,
		DocArticlesOfAssociation, DocProofOfAddress, DocProofOfCompanyActivity, DocBusinessBankStatement,
	} {
		d, ok := hasDoc(key)
		if !ok {
			t.Fatalf("business KYB missing document %q", key)
		}
		if !d.Required {
			t.Errorf("document %q should be required", key)
		}
	}

	// Importer → Proof of Imports required.
	if d, ok := hasDoc(DocProofOfImports); !ok || !d.Required {
		t.Error("importer business should require proof_of_imports")
	}
	// NRE → Proof of Wealth required.
	if d, ok := hasDoc(DocProofOfWealth); !ok || !d.Required {
		t.Error("EUR/GBP NRE business should require proof_of_wealth")
	}

	// Non-importer → Proof of Imports present but not required.
	if d, ok := hasDoc2(BuildIntakeRequirements("business", IntakeOptions{NeedsNRE: true}), DocProofOfImports); ok && d.Required {
		t.Error("non-importer should not require proof_of_imports")
	}
}

func hasDoc2(req IntakeRequirements, key string) (DocumentSpec, bool) {
	for _, d := range req.Documents {
		if d.Key == key {
			return d, true
		}
	}
	return DocumentSpec{}, false
}
